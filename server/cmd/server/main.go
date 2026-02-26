package main

import (
	"fmt"
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
	streamPort     = 9001
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
	ctrlServer.SetStreamPort(streamPort)

	// 3. On client connect → start the capture-encode-stream pipeline
	ctrlServer.OnClient(func(client control.ClientConn) {
		w := client.Hello.Screen.Width
		h := client.Hello.Screen.Height
		remoteAddr := client.Conn.RemoteAddr().String()
		host, _, _ := net.SplitHostPort(remoteAddr)
		targetAddr := fmt.Sprintf("%s:%d", host, streamPort)

		go startPipeline(w, h, targetAddr)
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
func startPipeline(width, height int, targetAddr string) {
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

	// H.264 encoder — forwards encoded NAL units to the streamer
	enc, err := encoder.New(encoder.Config{
		Width:   width,
		Height:  height,
		FPS:     defaultFPS,
		Bitrate: defaultBitrate,
	}, func(nal encoder.NALUnit) {
		ft := stream.FrameTypeP
		if nal.IsKeyframe {
			ft = stream.FrameTypeIDR
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
	select {} // Block forever (until process exits)
}
