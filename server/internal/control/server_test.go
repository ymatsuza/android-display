package control

import (
	"encoding/json"
	"net"
	"testing"
	"time"

	"github.com/luke/android-mac/server/internal/protocol"
)

func TestHandshake(t *testing.T) {
	srv, err := NewServer(0) // port 0 = random available port
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}
	defer srv.Stop()

	go srv.AcceptLoop()

	conn, err := net.DialTimeout("tcp", srv.Addr(), 2*time.Second)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer conn.Close()

	hello := protocol.ClientHello{
		Device:       "Tab S6 Lite",
		Screen:       protocol.ScreenInfo{Width: 2000, Height: 1200, DPI: 224},
		Capabilities: []string{"touch", "pen"},
		Codecs:       []string{"h264"},
	}
	encoder := json.NewEncoder(conn)
	if err := encoder.Encode(hello); err != nil {
		t.Fatalf("failed to send hello: %v", err)
	}

	var response protocol.ServerHello
	decoder := json.NewDecoder(conn)
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	if err := decoder.Decode(&response); err != nil {
		t.Fatalf("failed to read response: %v", err)
	}

	if response.Codec != "h264" {
		t.Errorf("expected codec h264, got %s", response.Codec)
	}
	if response.VirtualDisplay.Width != 2000 {
		t.Errorf("expected width 2000, got %d", response.VirtualDisplay.Width)
	}
	if response.StreamPort == 0 {
		t.Error("stream port should not be 0")
	}
}
