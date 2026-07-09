package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/luke/android-mac/server/internal/adb"
	"github.com/luke/android-mac/server/internal/capture"
	"github.com/luke/android-mac/server/internal/control"
	"github.com/luke/android-mac/server/internal/discovery"
	"github.com/luke/android-mac/server/internal/display"
	"github.com/luke/android-mac/server/internal/encoder"
	"github.com/luke/android-mac/server/internal/input"
	"github.com/luke/android-mac/server/internal/protocol"
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

// clientServers holds the per-client touch (and, for USB clients, video)
// servers created during that client's handshake.
type clientServers struct {
	touch *touch.Server
	video *stream.TCPVideoServer // nil for WiFi/UDP clients
}

// clientServersMap tracks per-client servers between allocation (during the
// handshake, in allocateClientPorts) and pipeline start (in OnClient).
var (
	clientServersMu  sync.Mutex
	clientServersMap = make(map[net.Conn]*clientServers)
)

// allocateClientPorts is invoked once per incoming connection during the
// handshake, before ServerHello is sent. It creates a fresh touch server
// (and, for USB clients, a TCP video server) dedicated to this client, and
// for USB clients broadcasts the new ports to every currently attached ADB
// serial via reverse forwarding (the control connection can't be correlated
// to a specific serial, so all attached devices get the forward — each
// Android client only ever dials the port it was given in its own
// ServerHello, so this is safe).
func allocateClientPorts(conn net.Conn, hello protocol.ClientHello) (touchPort, videoPort int, err error) {
	ts, err := touch.NewServer(0)
	if err != nil {
		return 0, 0, fmt.Errorf("touch server: %w", err)
	}
	go ts.AcceptLoop()

	var vs *stream.TCPVideoServer
	if hello.IsUSB() {
		vs, err = stream.NewTCPVideoServer(0)
		if err != nil {
			ts.Stop()
			return 0, 0, fmt.Errorf("video server: %w", err)
		}
	}

	clientServersMu.Lock()
	clientServersMap[conn] = &clientServers{touch: ts, video: vs}
	clientServersMu.Unlock()

	if hello.IsUSB() && adbManager != nil {
		serials, serr := adbManager.ListDeviceSerials()
		if serr != nil {
			log.Printf("ADB list devices failed: %v", serr)
		}
		for _, serial := range serials {
			if err := adbManager.SetupReverse(serial, ts.Port()); err != nil {
				log.Printf("ADB reverse touch port failed on %s: %v", serial, err)
			}
			if vs != nil {
				if err := adbManager.SetupReverse(serial, vs.Port()); err != nil {
					log.Printf("ADB reverse video port failed on %s: %v", serial, err)
				}
			}
		}
	}

	videoPort = 0
	if vs != nil {
		videoPort = vs.Port()
	}
	return ts.Port(), videoPort, nil
}

func main() {
	log.Println("android-mac server starting...")

	// 1. Set up ADB reverse forwarding for the control port on all currently
	// attached devices (USB touch/video ports are allocated per-client later).
	var err error
	adbManager, err = adb.NewManager()
	if err != nil {
		log.Printf("ADB not available: %v (USB connections disabled)", err)
	} else {
		serials, serr := adbManager.ListDeviceSerials()
		if serr == nil && len(serials) > 0 {
			for _, serial := range serials {
				if err := adbManager.RemoveAllReverse(serial); err != nil {
					log.Printf("ADB cleanup failed on %s: %v", serial, err)
				}
				if err := adbManager.SetupReverse(serial, controlPort); err != nil {
					log.Printf("ADB reverse control port failed on %s: %v", serial, err)
				}
			}
			log.Printf("ADB reverse forwarding enabled for %d device(s)", len(serials))
		} else {
			log.Println("ADB available but no device connected")
		}

		// Reverse forwards live in the device's adbd and are lost when the
		// USB link drops, so devices attached or replugged after startup need
		// the control-port forward re-established. Without this a replugged
		// device gets "Connection Failed" until the server restarts.
		watcher := adb.NewWatcher(adbManager.ListDeviceSerials, serials, func(serial string) {
			log.Printf("ADB device attached: %s — setting up reverse forwarding", serial)
			if err := adbManager.RemoveAllReverse(serial); err != nil {
				log.Printf("ADB cleanup failed on %s: %v", serial, err)
			}
			if err := adbManager.SetupReverse(serial, controlPort); err != nil {
				log.Printf("ADB reverse control port failed on %s: %v", serial, err)
			}
		})
		go watcher.Run(2 * time.Second)
		defer watcher.Stop()
	}

	// 2. Start mDNS advertisement
	hostname, _ := os.Hostname()
	mdns, err := discovery.NewService(hostname, controlPort)
	if err != nil {
		log.Fatalf("mDNS failed: %v", err)
	}
	defer mdns.Stop()
	log.Printf("mDNS advertising on port %d", controlPort)

	// 3. Start TCP control server
	ctrlServer, err := control.NewServer(controlPort)
	if err != nil {
		log.Fatalf("control server failed: %v", err)
	}
	defer ctrlServer.Stop()
	ctrlServer.SetPortAllocator(allocateClientPorts)

	// 4. On client connect → start the capture-encode-stream pipeline
	ctrlServer.OnClient(func(client control.ClientConn) {
		w := client.Hello.Screen.Width
		h := client.Hello.Screen.Height
		bitrate := client.Hello.Bitrate
		if bitrate <= 0 {
			bitrate = defaultBitrate
		}
		log.Printf("client requested bitrate: %d bps", bitrate)

		clientServersMu.Lock()
		cs := clientServersMap[client.Conn]
		delete(clientServersMap, client.Conn)
		clientServersMu.Unlock()
		if cs == nil {
			log.Println("no per-client servers allocated, dropping client")
			return
		}

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

		// Tear down this client's per-client servers and reverse forwards
		// once the pipeline ends.
		go func() {
			<-ctx.Done()
			cs.touch.Stop()
			if cs.video != nil {
				cs.video.Close()
			}
			if client.Hello.IsUSB() && adbManager != nil {
				serials, err := adbManager.ListDeviceSerials()
				if err != nil {
					log.Printf("ADB list devices failed during cleanup: %v", err)
					return
				}
				for _, serial := range serials {
					if err := adbManager.RemoveReverse(serial, cs.touch.Port()); err != nil {
						log.Printf("ADB remove touch reverse failed on %s: %v", serial, err)
					}
					if cs.video != nil {
						if err := adbManager.RemoveReverse(serial, cs.video.Port()); err != nil {
							log.Printf("ADB remove video reverse failed on %s: %v", serial, err)
						}
					}
				}
			}
		}()

		// Determine video transport based on connection type
		if client.Hello.IsUSB() {
			go startPipelineUSB(ctx, cancel, w, h, bitrate, cs.touch, cs.video)
		} else {
			remoteAddr := client.Conn.RemoteAddr().String()
			host, _, _ := net.SplitHostPort(remoteAddr)
			targetAddr := fmt.Sprintf("%s:%d", host, client.UDPPort)
			go startPipelineWiFi(ctx, cancel, w, h, bitrate, targetAddr, cs.touch)
		}
	})

	go ctrlServer.AcceptLoop()
	log.Printf("control server on port %d — waiting for connections...", controlPort)

	// 5. Wait for SIGINT/SIGTERM
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	log.Println("shutting down...")

	// Clean up ADB reverse forwarding on all attached devices
	if adbManager != nil {
		if serials, err := adbManager.ListDeviceSerials(); err == nil {
			for _, serial := range serials {
				adbManager.RemoveAllReverse(serial)
			}
		}
	}
}

// startPipelineWiFi starts the capture-encode-stream pipeline using UDP transport.
func startPipelineWiFi(ctx context.Context, cancel context.CancelFunc, width, height, bitrate int, targetAddr string, touchServer *touch.Server) {
	defer cancel()
	log.Printf("starting WiFi pipeline: %dx%d @ %d bps → %s", width, height, bitrate, targetAddr)

	// UDP streamer
	udpStreamer, err := stream.NewUDPStreamer(targetAddr)
	if err != nil {
		log.Printf("UDP streamer error: %v", err)
		return
	}
	defer udpStreamer.Close()

	startPipelineCommon(ctx, cancel, width, height, bitrate, udpStreamer, touchServer)
}

// startPipelineUSB starts the capture-encode-stream pipeline using TCP transport.
func startPipelineUSB(ctx context.Context, cancel context.CancelFunc, width, height, bitrate int, touchServer *touch.Server, videoServer *stream.TCPVideoServer) {
	defer cancel()
	log.Printf("starting USB pipeline: %dx%d @ %d bps (TCP video)", width, height, bitrate)

	if videoServer == nil {
		log.Println("no TCP video server allocated for USB client")
		return
	}

	// Wait for client to connect to TCP video port
	if err := videoServer.AcceptOne(); err != nil {
		log.Printf("TCP video accept error: %v", err)
		return
	}

	startPipelineCommon(ctx, cancel, width, height, bitrate, videoServer, touchServer)
}

// startPipelineCommon contains the shared pipeline logic for both WiFi and USB modes.
func startPipelineCommon(ctx context.Context, cancel context.CancelFunc, width, height, bitrate int, videoStreamer stream.Streamer, touchServer *touch.Server) {
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
		Bitrate: bitrate,
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
