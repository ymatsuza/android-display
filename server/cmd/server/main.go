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
	ctrlServer.SetTouchPort(touchServer.Port())

	// 4. On client connect → start the capture-encode-stream pipeline
	ctrlServer.OnClient(func(client control.ClientConn) {
		w := client.Hello.Screen.Width
		h := client.Hello.Screen.Height
		remoteAddr := client.Conn.RemoteAddr().String()
		host, _, _ := net.SplitHostPort(remoteAddr)
		targetAddr := fmt.Sprintf("%s:%d", host, client.UDPPort)

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

		go startPipeline(ctx, cancel, w, h, targetAddr, touchServer)
	})

	go ctrlServer.AcceptLoop()
	log.Printf("control server on port %d — waiting for connections...", controlPort)

	// 5. Wait for SIGINT/SIGTERM
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	log.Println("shutting down...")
}

func startPipeline(ctx context.Context, cancel context.CancelFunc, width, height int, targetAddr string, touchServer *touch.Server) {
	defer cancel()
	log.Printf("starting pipeline: %dx%d → %s", width, height, targetAddr)

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

	// UDP streamer
	streamer, err := stream.NewUDPStreamer(targetAddr)
	if err != nil {
		log.Printf("streamer error: %v", err)
		return
	}
	defer streamer.Close()

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
		if err := streamer.SendFrame(nal.Data, ft); err != nil {
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
