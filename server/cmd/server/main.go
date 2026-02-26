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
	"github.com/luke/android-mac/server/internal/stream"
)

const (
	controlPort    = 9000
	defaultFPS     = 60
	defaultBitrate = 8_000_000
)

func main() {
	log.Println("android-mac server starting...")

	// 1. Start mDNS advertisement
	hostname, _ := os.Hostname()
	mdns, err := discovery.NewService(hostname, controlPort)
	if err != nil {
		log.Fatalf("mDNS failed: %v", err)
	}
	defer mdns.Stop()
	log.Printf("mDNS advertising on port %d", controlPort)

	// 2. Start TCP control server
	ctrlServer, err := control.NewServer(controlPort)
	if err != nil {
		log.Fatalf("control server failed: %v", err)
	}
	defer ctrlServer.Stop()

	// 3. On client connect → start the capture-encode-stream pipeline
	ctrlServer.OnClient(func(client control.ClientConn) {
		w := client.Hello.Screen.Width
		h := client.Hello.Screen.Height
		remoteAddr := client.Conn.RemoteAddr().String()
		host, _, _ := net.SplitHostPort(remoteAddr)
		// C1: Use the client-reported UDP port instead of a hardcoded one.
		targetAddr := fmt.Sprintf("%s:%d", host, client.UDPPort)

		// C2: Create a cancellable context for the pipeline.
		ctx, cancel := context.WithCancel(context.Background())

		// Monitor the TCP control connection — when it closes, cancel the pipeline.
		go func() {
			buf := make([]byte, 1)
			for {
				// TODO (I2): Implement heartbeat echo here instead of bare read.
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

		go startPipeline(ctx, cancel, w, h, targetAddr)
	})

	go ctrlServer.AcceptLoop()
	log.Printf("control server on port %d — waiting for connections...", controlPort)

	// 4. Wait for SIGINT/SIGTERM to shut down
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	log.Println("shutting down...")
}

// startPipeline creates a virtual display, captures it, encodes frames
// to H.264, and streams the NAL units over UDP to the client.
// It blocks until ctx is cancelled (e.g. client disconnect), then all
// deferred cleanup runs.
func startPipeline(ctx context.Context, cancel context.CancelFunc, width, height int, targetAddr string) {
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

	// UDP streamer
	streamer, err := stream.NewUDPStreamer(targetAddr)
	if err != nil {
		log.Printf("streamer error: %v", err)
		return
	}
	defer streamer.Close()

	// C3+I1: H.264 encoder — map NAL types for proper SPS/PPS/IDR/P classification
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

	// Screen capture — feeds raw frames into the encoder
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

	log.Println("pipeline active")
	// C2: Block until context is cancelled (client disconnect or signal),
	// then all deferred cleanup runs.
	<-ctx.Done()
	log.Println("pipeline stopping...")
}
