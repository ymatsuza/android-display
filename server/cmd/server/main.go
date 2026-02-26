package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/luke/android-mac/server/internal/adb"
	"github.com/luke/android-mac/server/internal/capture"
	"github.com/luke/android-mac/server/internal/control"
	"github.com/luke/android-mac/server/internal/discovery"
	"github.com/luke/android-mac/server/internal/display"
	"github.com/luke/android-mac/server/internal/encoder"
	"github.com/luke/android-mac/server/internal/input"
	"github.com/luke/android-mac/server/internal/stream"
	"github.com/luke/android-mac/server/internal/touch"
)

const (
	controlPort    = 9000
	defaultFPS     = 60
	defaultBitrate = 8_000_000
)

// adbManager is set up at startup if ADB is available.
var adbManager *adb.Manager

// tcpVideoServer is always started for USB mode support.
var tcpVideoServer *stream.TCPVideoServer

func main() {
	log.Println("android-mac server starting...")

	// 1. Start touch TCP server (auto-assign port)
	touchServer, err := touch.NewServer(0)
	if err != nil {
		log.Fatalf("touch server failed: %v", err)
	}
	defer touchServer.Stop()
	go touchServer.AcceptLoop()
	log.Printf("touch server on port %d", touchServer.Port())

	// 2. Start TCP video server for USB mode (auto-assign port)
	tcpVideoServer, err = stream.NewTCPVideoServer(0)
	if err != nil {
		log.Fatalf("TCP video server failed: %v", err)
	}
	defer tcpVideoServer.Close()
	log.Printf("TCP video server on port %d", tcpVideoServer.Port())

	// 3. Set up ADB reverse forwarding if device is connected
	adbManager, err = adb.NewManager()
	if err != nil {
		log.Printf("ADB not available: %v (USB connections disabled)", err)
	} else if adbManager.HasDevice() {
		// Clean up any stale reverse forwarding from previous runs
		adbManager.RemoveAllReverse()

		// Set up reverse forwarding for all ports
		if err := adbManager.SetupReverse(controlPort); err != nil {
			log.Printf("ADB reverse control port failed: %v", err)
		}
		if err := adbManager.SetupReverse(touchServer.Port()); err != nil {
			log.Printf("ADB reverse touch port failed: %v", err)
		}
		if err := adbManager.SetupReverse(tcpVideoServer.Port()); err != nil {
			log.Printf("ADB reverse video port failed: %v", err)
		}
		log.Println("ADB reverse forwarding enabled (USB connections ready)")
	} else {
		log.Println("ADB available but no device connected")
	}

	// 4. Start mDNS advertisement
	hostname, _ := os.Hostname()
	mdns, err := discovery.NewService(hostname, controlPort)
	if err != nil {
		log.Fatalf("mDNS failed: %v", err)
	}
	defer mdns.Stop()
	log.Printf("mDNS advertising on port %d", controlPort)

	// 5. Start TCP control server
	ctrlServer, err := control.NewServer(controlPort)
	if err != nil {
		log.Fatalf("control server failed: %v", err)
	}
	defer ctrlServer.Stop()
	ctrlServer.SetTouchPort(touchServer.Port())
	ctrlServer.SetVideoPort(tcpVideoServer.Port())

	// 6. On client connect → start the capture-encode-stream pipeline
	ctrlServer.OnClient(func(client control.ClientConn) {
		w := client.Hello.Screen.Width
		h := client.Hello.Screen.Height

		ctx, cancel := context.WithCancel(context.Background())

		// Monitor the TCP control connection
		go func() {
			buf := make([]byte, 1)
			for {
				_, err := client.Conn.Read(buf)
				if err != nil {
					if err != io.EOF {
						log.Printf("control connection read error: %v", err)
					}
					log.Println("client disconnected, cancelling pipeline")
					cancel()
					return
				}
			}
		}()

		// Determine video transport based on connection type
		if client.Hello.IsUSB() {
			go startPipelineUSB(ctx, cancel, w, h, touchServer)
		} else {
			remoteAddr := client.Conn.RemoteAddr().String()
			host, _, _ := net.SplitHostPort(remoteAddr)
			targetAddr := fmt.Sprintf("%s:%d", host, client.UDPPort)
			go startPipelineWiFi(ctx, cancel, w, h, targetAddr, touchServer)
		}
	})

	go ctrlServer.AcceptLoop()
	log.Printf("control server on port %d — waiting for connections...", controlPort)

	// 7. Wait for SIGINT/SIGTERM
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	log.Println("shutting down...")

	// Clean up ADB reverse forwarding
	if adbManager != nil {
		adbManager.RemoveAllReverse()
	}
}

// startPipelineWiFi starts the capture-encode-stream pipeline using UDP transport.
func startPipelineWiFi(ctx context.Context, cancel context.CancelFunc, width, height int, targetAddr string, touchServer *touch.Server) {
	defer cancel()
	log.Printf("starting WiFi pipeline: %dx%d → %s", width, height, targetAddr)

	// UDP streamer
	udpStreamer, err := stream.NewUDPStreamer(targetAddr)
	if err != nil {
		log.Printf("UDP streamer error: %v", err)
		return
	}
	defer udpStreamer.Close()

	startPipelineCommon(ctx, cancel, width, height, udpStreamer, touchServer)
}

// startPipelineUSB starts the capture-encode-stream pipeline using TCP transport.
func startPipelineUSB(ctx context.Context, cancel context.CancelFunc, width, height int, touchServer *touch.Server) {
	defer cancel()
	log.Printf("starting USB pipeline: %dx%d (TCP video)", width, height)

	// Wait for client to connect to TCP video port
	if err := tcpVideoServer.AcceptOne(); err != nil {
		log.Printf("TCP video accept error: %v", err)
		return
	}

	startPipelineCommon(ctx, cancel, width, height, tcpVideoServer, touchServer)
}

// startPipelineCommon contains the shared pipeline logic for both WiFi and USB modes.
func startPipelineCommon(ctx context.Context, cancel context.CancelFunc, width, height int, videoStreamer stream.Streamer, touchServer *touch.Server) {
	// Virtual display
	vd, err := display.New(display.Config{
		Width:  width,
		Height: height,
		PPI:    224,
	})
	if err != nil {
		log.Printf("virtual display error: %v", err)
		return
	}
	defer vd.Close()
	log.Printf("virtual display created: 0x%x (%dx%d)", vd.DisplayID(), width, height)

	// Input injector + gesture recognizer for this display
	injector := input.NewInjector(vd.DisplayID())
	gesture := input.NewGestureRecognizer(func(me input.MouseEvent) {
		switch me.Action {
		case input.ActionMouseMove:
			injector.MouseMove(me.X, me.Y)
		case input.ActionLeftDown:
			injector.LeftMouseDown(me.X, me.Y)
		case input.ActionLeftUp:
			injector.LeftMouseUp(me.X, me.Y)
		case input.ActionLeftDragged:
			injector.LeftMouseDragged(me.X, me.Y)
		case input.ActionRightDown:
			injector.RightMouseDown(me.X, me.Y)
		case input.ActionRightUp:
			injector.RightMouseUp(me.X, me.Y)
		case input.ActionScroll:
			injector.ScrollWheel(me.ScrollX, me.ScrollY)
		}
	})
	defer gesture.Close()

	// Wire touch events → pen events bypass gesture recognizer
	touchServer.OnEvent(func(e touch.Event) {
		if e.Type == touch.TouchTypePen {
			switch e.Action {
			case touch.TouchActionDown:
				injector.TabletProximityEnter()
				injector.TabletDown(e.X, e.Y, e.Pressure, e.TiltX, e.TiltY)
			case touch.TouchActionMove:
				injector.TabletDragged(e.X, e.Y, e.Pressure, e.TiltX, e.TiltY)
			case touch.TouchActionUp:
				injector.TabletUp(e.X, e.Y, e.Pressure, e.TiltX, e.TiltY)
				injector.TabletProximityLeave()
			}
		} else {
			gesture.HandleEvent(e)
		}
	})
	defer touchServer.OnEvent(nil)
	log.Println("touch input enabled")

	// H.264 encoder
	enc, err := encoder.New(encoder.Config{
		Width:   width,
		Height:  height,
		FPS:     defaultFPS,
		Bitrate: defaultBitrate,
	}, func(nal encoder.NALUnit) {
		var ft byte
		switch {
		case nal.NALType == 7:
			ft = stream.FrameTypeSPS
		case nal.NALType == 8:
			ft = stream.FrameTypePPS
		case nal.IsKeyframe:
			ft = stream.FrameTypeIDR
		default:
			ft = stream.FrameTypeP
		}
		if err := videoStreamer.SendFrame(nal.Data, ft); err != nil {
			log.Printf("stream send error: %v", err)
		}
	})
	if err != nil {
		log.Printf("encoder error: %v", err)
		return
	}
	defer enc.Close()

	// Screen capture
	cap, err := capture.Start(vd.DisplayID(), defaultFPS, func(frame capture.Frame) {
		defer capture.ReleasePixelBuffer(frame.PixelBuffer)
		if err := enc.Encode(frame.PixelBuffer, frame.Timestamp); err != nil {
			log.Printf("encode error: %v", err)
		}
	})
	if err != nil {
		log.Printf("capture error: %v", err)
		return
	}
	defer cap.Stop()

	log.Println("pipeline active (video + touch)")
	<-ctx.Done()
	log.Println("pipeline stopping...")
}
