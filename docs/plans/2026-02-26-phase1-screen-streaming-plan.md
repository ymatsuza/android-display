# Phase 1: Screen Streaming Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Mac virtual display visible on Android tablet over WiFi — the complete video pipeline from Mac screen capture to Android display.

**Architecture:** Go server on Mac creates a virtual display, captures its frames via ScreenCaptureKit, encodes to H.264 via VideoToolbox, and streams over UDP to an Android client that decodes with MediaCodec and renders to a fullscreen SurfaceView. mDNS handles device discovery; TCP handles connection handshake.

**Tech Stack:** Go 1.22+ (Mac server), CGo (macOS framework bridging), Kotlin (Android client), Protobuf (serialization), H.264 (video codec), UDP (video transport), TCP (control), mDNS/Bonjour (discovery)

---

## Project Structure

```
android-mac/
├── server/                     # Mac Go server
│   ├── go.mod
│   ├── go.sum
│   ├── main.go
│   ├── cmd/
│   │   └── server/
│   │       └── main.go         # Entry point
│   ├── internal/
│   │   ├── display/
│   │   │   ├── virtual.go      # CGo: CGVirtualDisplay
│   │   │   └── virtual.h       # C header for virtual display
│   │   ├── capture/
│   │   │   ├── capture.go      # CGo: ScreenCaptureKit
│   │   │   └── capture.h       # C header for screen capture
│   │   ├── encoder/
│   │   │   ├── h264.go         # CGo: VideoToolbox H.264
│   │   │   └── h264.h          # C header for encoder
│   │   ├── stream/
│   │   │   ├── packet.go       # Video packet format
│   │   │   ├── packet_test.go  # Packet format tests
│   │   │   ├── udp.go          # UDP video streamer
│   │   │   └── udp_test.go     # UDP streamer tests
│   │   ├── control/
│   │   │   ├── server.go       # TCP control server
│   │   │   ├── server_test.go  # Control server tests
│   │   │   ├── handshake.go    # Handshake protocol
│   │   │   └── handshake_test.go
│   │   ├── discovery/
│   │   │   ├── mdns.go         # mDNS/Bonjour advertisement
│   │   │   └── mdns_test.go
│   │   └── protocol/
│   │       └── messages.go     # Shared message types (JSON)
│   └── bridge/                 # Objective-C bridge files
│       ├── display_bridge.m    # ObjC implementation for virtual display
│       ├── display_bridge.h
│       ├── capture_bridge.m    # ObjC implementation for screen capture
│       ├── capture_bridge.h
│       ├── encoder_bridge.m    # ObjC implementation for H.264 encoding
│       └── encoder_bridge.h
├── client/                     # Android Kotlin client
│   ├── app/
│   │   ├── build.gradle.kts
│   │   └── src/main/
│   │       ├── AndroidManifest.xml
│   │       ├── java/com/androidmac/client/
│   │       │   ├── MainActivity.kt
│   │       │   ├── discovery/
│   │       │   │   └── NsdDiscovery.kt
│   │       │   ├── control/
│   │       │   │   └── ControlClient.kt
│   │       │   ├── video/
│   │       │   │   ├── UdpReceiver.kt
│   │       │   │   └── VideoDecoder.kt
│   │       │   └── protocol/
│   │       │       └── Messages.kt
│   │       └── res/
│   │           ├── layout/
│   │           │   ├── activity_main.xml
│   │           │   └── activity_display.xml
│   │           └── values/
│   │               └── strings.xml
│   ├── build.gradle.kts
│   ├── settings.gradle.kts
│   └── gradle.properties
├── proto/
│   └── touch.proto             # Protobuf definitions (Phase 2)
└── docs/plans/
```

---

## Task 1: Go Project Setup

**Files:**
- Create: `server/go.mod`
- Create: `server/cmd/server/main.go`
- Create: `server/internal/protocol/messages.go`

**Step 1: Initialize Go module**

```bash
cd /Users/luke/sideProject/android-mac
mkdir -p server/cmd/server server/internal/protocol
cd server
go mod init github.com/luke/android-mac/server
```

**Step 2: Create shared protocol message types**

Create `server/internal/protocol/messages.go`:

```go
package protocol

// ClientHello is sent by Android to Mac during handshake
type ClientHello struct {
	Device       string       `json:"device"`
	Screen       ScreenInfo   `json:"screen"`
	Capabilities []string     `json:"capabilities"`
	Codecs       []string     `json:"codecs"`
}

type ScreenInfo struct {
	Width  int `json:"width"`
	Height int `json:"height"`
	DPI    int `json:"dpi"`
}

// ServerHello is sent by Mac to Android in response
type ServerHello struct {
	VirtualDisplay DisplayInfo `json:"virtualDisplay"`
	Codec          string      `json:"codec"`
	Bitrate        int         `json:"bitrate"`
	FPS            int         `json:"fps"`
	StreamPort     int         `json:"streamPort"`
}

type DisplayInfo struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

// Heartbeat is sent periodically on the control channel
type Heartbeat struct {
	Timestamp int64 `json:"timestamp"`
}
```

**Step 3: Create minimal main.go placeholder**

Create `server/cmd/server/main.go`:

```go
package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	fmt.Println("android-mac server starting...")

	// Wait for interrupt
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig

	fmt.Println("shutting down...")
}
```

**Step 4: Verify build**

```bash
cd /Users/luke/sideProject/android-mac/server
go build ./cmd/server/
```
Expected: Builds without errors.

**Step 5: Commit**

```bash
git add server/
git commit -m "feat: initialize Go server project with protocol types"
```

---

## Task 2: Video Packet Format (Pure Go, Testable)

**Files:**
- Create: `server/internal/stream/packet.go`
- Create: `server/internal/stream/packet_test.go`

**Step 1: Write failing tests for packet serialization**

Create `server/internal/stream/packet_test.go`:

```go
package stream

import (
	"bytes"
	"testing"
)

func TestPacketHeaderSerialize(t *testing.T) {
	hdr := PacketHeader{
		Sequence:  42,
		Timestamp: 1234567890,
		FrameType: FrameTypeIDR,
		FragIndex: 0,
		FragTotal: 1,
	}

	buf := hdr.Marshal()
	if len(buf) != PacketHeaderSize {
		t.Fatalf("expected header size %d, got %d", PacketHeaderSize, len(buf))
	}

	var decoded PacketHeader
	err := decoded.Unmarshal(buf)
	if err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if decoded.Sequence != hdr.Sequence {
		t.Errorf("sequence: got %d, want %d", decoded.Sequence, hdr.Sequence)
	}
	if decoded.Timestamp != hdr.Timestamp {
		t.Errorf("timestamp: got %d, want %d", decoded.Timestamp, hdr.Timestamp)
	}
	if decoded.FrameType != hdr.FrameType {
		t.Errorf("frameType: got %d, want %d", decoded.FrameType, hdr.FrameType)
	}
}

func TestSplitNALUnits(t *testing.T) {
	// Create test data larger than MaxPayloadSize
	data := bytes.Repeat([]byte{0xAB}, MaxPayloadSize*2+100)

	packets := SplitIntoPackets(1, 999, FrameTypeIDR, data)

	if len(packets) != 3 {
		t.Fatalf("expected 3 packets, got %d", len(packets))
	}

	// Check fragment indices
	for i, pkt := range packets {
		if pkt.Header.FragIndex != uint8(i) {
			t.Errorf("packet %d: fragIndex got %d, want %d", i, pkt.Header.FragIndex, i)
		}
		if pkt.Header.FragTotal != 3 {
			t.Errorf("packet %d: fragTotal got %d, want 3", i, pkt.Header.FragTotal)
		}
	}

	// Reassemble and verify
	var reassembled []byte
	for _, pkt := range packets {
		reassembled = append(reassembled, pkt.Payload...)
	}
	if !bytes.Equal(reassembled, data) {
		t.Error("reassembled data does not match original")
	}
}

func TestSmallPayloadSinglePacket(t *testing.T) {
	data := []byte{0x01, 0x02, 0x03}
	packets := SplitIntoPackets(1, 999, FrameTypeP, data)

	if len(packets) != 1 {
		t.Fatalf("expected 1 packet, got %d", len(packets))
	}
	if packets[0].Header.FragIndex != 0 || packets[0].Header.FragTotal != 1 {
		t.Error("single packet should have fragIndex=0, fragTotal=1")
	}
}
```

**Step 2: Run test to verify it fails**

```bash
cd /Users/luke/sideProject/android-mac/server
go test ./internal/stream/ -v
```
Expected: FAIL — types not defined.

**Step 3: Implement packet format**

Create `server/internal/stream/packet.go`:

```go
package stream

import (
	"encoding/binary"
	"errors"
)

const (
	// PacketHeaderSize is the fixed header size in bytes
	// Sequence(4) + Timestamp(8) + FrameType(1) + FragIndex(1) + FragTotal(1) = 15
	PacketHeaderSize = 15

	// MaxPayloadSize keeps UDP packets under typical MTU (~1400 bytes for WiFi)
	MaxPayloadSize = 1400 - PacketHeaderSize

	FrameTypeIDR byte = 0x01 // I-frame (keyframe)
	FrameTypeP   byte = 0x02 // P-frame
	FrameTypeB   byte = 0x03 // B-frame
	FrameTypeSPS byte = 0x10 // SPS NAL unit
	FrameTypePPS byte = 0x11 // PPS NAL unit
)

// PacketHeader is the fixed-size header prepended to each UDP packet
type PacketHeader struct {
	Sequence  uint32 // Monotonically increasing sequence number
	Timestamp uint64 // Capture timestamp in microseconds
	FrameType byte   // Type of NAL unit / frame
	FragIndex uint8  // Fragment index (0-based)
	FragTotal uint8  // Total fragments for this frame
}

// Packet is a complete UDP packet with header + payload
type Packet struct {
	Header  PacketHeader
	Payload []byte
}

// Marshal serializes the header to bytes (big-endian)
func (h *PacketHeader) Marshal() []byte {
	buf := make([]byte, PacketHeaderSize)
	binary.BigEndian.PutUint32(buf[0:4], h.Sequence)
	binary.BigEndian.PutUint64(buf[4:12], h.Timestamp)
	buf[12] = h.FrameType
	buf[13] = h.FragIndex
	buf[14] = h.FragTotal
	return buf
}

// Unmarshal deserializes the header from bytes
func (h *PacketHeader) Unmarshal(buf []byte) error {
	if len(buf) < PacketHeaderSize {
		return errors.New("buffer too small for packet header")
	}
	h.Sequence = binary.BigEndian.Uint32(buf[0:4])
	h.Timestamp = binary.BigEndian.Uint64(buf[4:12])
	h.FrameType = buf[12]
	h.FragIndex = buf[13]
	h.FragTotal = buf[14]
	return nil
}

// MarshalPacket serializes a complete packet (header + payload)
func (p *Packet) MarshalPacket() []byte {
	hdr := p.Header.Marshal()
	return append(hdr, p.Payload...)
}

// UnmarshalPacket deserializes a complete packet from raw UDP data
func UnmarshalPacket(data []byte) (*Packet, error) {
	var hdr PacketHeader
	if err := hdr.Unmarshal(data); err != nil {
		return nil, err
	}
	return &Packet{
		Header:  hdr,
		Payload: data[PacketHeaderSize:],
	}, nil
}

// SplitIntoPackets splits a NAL unit into multiple UDP packets if needed
func SplitIntoPackets(seq uint32, timestamp uint64, frameType byte, data []byte) []Packet {
	if len(data) <= MaxPayloadSize {
		return []Packet{{
			Header: PacketHeader{
				Sequence:  seq,
				Timestamp: timestamp,
				FrameType: frameType,
				FragIndex: 0,
				FragTotal: 1,
			},
			Payload: data,
		}}
	}

	total := (len(data) + MaxPayloadSize - 1) / MaxPayloadSize
	packets := make([]Packet, 0, total)

	for i := 0; i < total; i++ {
		start := i * MaxPayloadSize
		end := start + MaxPayloadSize
		if end > len(data) {
			end = len(data)
		}
		packets = append(packets, Packet{
			Header: PacketHeader{
				Sequence:  seq,
				Timestamp: timestamp,
				FrameType: frameType,
				FragIndex: uint8(i),
				FragTotal: uint8(total),
			},
			Payload: data[start:end],
		})
	}

	return packets
}
```

**Step 4: Run tests to verify they pass**

```bash
cd /Users/luke/sideProject/android-mac/server
go test ./internal/stream/ -v
```
Expected: All PASS.

**Step 5: Commit**

```bash
git add server/internal/stream/
git commit -m "feat: add video packet format with fragmentation support"
```

---

## Task 3: mDNS Service Advertisement

**Files:**
- Create: `server/internal/discovery/mdns.go`
- Create: `server/internal/discovery/mdns_test.go`

**Step 1: Add dependency**

```bash
cd /Users/luke/sideProject/android-mac/server
go get github.com/grandcat/zeroconf
```

**Step 2: Write failing test**

Create `server/internal/discovery/mdns_test.go`:

```go
package discovery

import (
	"context"
	"testing"
	"time"

	"github.com/grandcat/zeroconf"
)

func TestAdvertiseAndDiscover(t *testing.T) {
	// Start advertising
	svc, err := NewService("TestMac", 9000)
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}
	defer svc.Stop()

	// Discover
	resolver, err := zeroconf.NewResolver(nil)
	if err != nil {
		t.Fatalf("failed to create resolver: %v", err)
	}

	entries := make(chan *zeroconf.ServiceEntry)
	found := false

	go func() {
		for entry := range entries {
			if entry.Instance == "TestMac" {
				found = true
			}
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	err = resolver.Browse(ctx, ServiceType, "local.", entries)
	if err != nil {
		t.Fatalf("browse error: %v", err)
	}
	<-ctx.Done()

	if !found {
		t.Error("did not discover advertised service")
	}
}
```

**Step 3: Run test to verify it fails**

```bash
go test ./internal/discovery/ -v -timeout 10s
```
Expected: FAIL — NewService not defined.

**Step 4: Implement mDNS service**

Create `server/internal/discovery/mdns.go`:

```go
package discovery

import "github.com/grandcat/zeroconf"

const ServiceType = "_androidmac._tcp"

// Service wraps zeroconf mDNS advertisement
type Service struct {
	server *zeroconf.Server
}

// NewService starts advertising a service on the given port
func NewService(instanceName string, port int) (*Service, error) {
	server, err := zeroconf.Register(
		instanceName,
		ServiceType,
		"local.",
		port,
		[]string{"version=1.0"},
		nil,
	)
	if err != nil {
		return nil, err
	}
	return &Service{server: server}, nil
}

// Stop shuts down the mDNS advertisement
func (s *Service) Stop() {
	if s.server != nil {
		s.server.Shutdown()
	}
}
```

**Step 5: Run test to verify it passes**

```bash
go test ./internal/discovery/ -v -timeout 10s
```
Expected: PASS.

**Step 6: Commit**

```bash
git add server/internal/discovery/ server/go.mod server/go.sum
git commit -m "feat: add mDNS service discovery via zeroconf"
```

---

## Task 4: TCP Control Server + Handshake

**Files:**
- Create: `server/internal/control/server.go`
- Create: `server/internal/control/server_test.go`

**Step 1: Write failing tests**

Create `server/internal/control/server_test.go`:

```go
package control

import (
	"encoding/json"
	"net"
	"testing"
	"time"

	"github.com/luke/android-mac/server/internal/protocol"
)

func TestHandshake(t *testing.T) {
	// Start control server
	srv, err := NewServer(0) // port 0 = random available port
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}
	defer srv.Stop()

	go srv.AcceptLoop()

	// Simulate Android client connecting
	conn, err := net.DialTimeout("tcp", srv.Addr(), 2*time.Second)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer conn.Close()

	// Send ClientHello
	hello := protocol.ClientHello{
		Device: "Tab S6 Lite",
		Screen: protocol.ScreenInfo{Width: 2000, Height: 1200, DPI: 224},
		Capabilities: []string{"touch", "pen"},
		Codecs: []string{"h264"},
	}
	encoder := json.NewEncoder(conn)
	if err := encoder.Encode(hello); err != nil {
		t.Fatalf("failed to send hello: %v", err)
	}

	// Read ServerHello response
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
```

**Step 2: Run test to verify it fails**

```bash
go test ./internal/control/ -v -timeout 10s
```
Expected: FAIL — NewServer not defined.

**Step 3: Implement control server**

Create `server/internal/control/server.go`:

```go
package control

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"sync"

	"github.com/luke/android-mac/server/internal/protocol"
)

// ClientConn represents a connected Android client
type ClientConn struct {
	Conn  net.Conn
	Hello protocol.ClientHello
}

// Server is the TCP control channel server
type Server struct {
	listener   net.Listener
	clients    []ClientConn
	mu         sync.Mutex
	streamPort int
	onClient   func(ClientConn)
	done       chan struct{}
}

// NewServer creates a control server on the given port (0 for random)
func NewServer(port int) (*Server, error) {
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return nil, err
	}
	return &Server{
		listener:   ln,
		streamPort: 5001, // default UDP stream port
		done:       make(chan struct{}),
	}, nil
}

// Addr returns the address the server is listening on
func (s *Server) Addr() string {
	return s.listener.Addr().String()
}

// Port returns the port number the server is listening on
func (s *Server) Port() int {
	return s.listener.Addr().(*net.TCPAddr).Port
}

// SetStreamPort sets the UDP port that will be communicated to clients
func (s *Server) SetStreamPort(port int) {
	s.streamPort = port
}

// OnClient sets a callback for when a new client completes handshake
func (s *Server) OnClient(fn func(ClientConn)) {
	s.onClient = fn
}

// AcceptLoop accepts incoming connections and handles handshake
func (s *Server) AcceptLoop() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			select {
			case <-s.done:
				return
			default:
				log.Printf("accept error: %v", err)
				continue
			}
		}
		go s.handleConn(conn)
	}
}

func (s *Server) handleConn(conn net.Conn) {
	// Read ClientHello
	var hello protocol.ClientHello
	decoder := json.NewDecoder(conn)
	if err := decoder.Decode(&hello); err != nil {
		log.Printf("handshake read error: %v", err)
		conn.Close()
		return
	}

	log.Printf("client connected: %s (%dx%d)", hello.Device, hello.Screen.Width, hello.Screen.Height)

	// Choose codec (prefer h264 for now)
	codec := "h264"
	for _, c := range hello.Codecs {
		if c == "h264" {
			codec = "h264"
			break
		}
	}

	// Send ServerHello
	response := protocol.ServerHello{
		VirtualDisplay: protocol.DisplayInfo{
			Width:  hello.Screen.Width,
			Height: hello.Screen.Height,
		},
		Codec:      codec,
		Bitrate:    8_000_000, // 8 Mbps
		FPS:        60,
		StreamPort: s.streamPort,
	}

	encoder := json.NewEncoder(conn)
	if err := encoder.Encode(response); err != nil {
		log.Printf("handshake write error: %v", err)
		conn.Close()
		return
	}

	client := ClientConn{Conn: conn, Hello: hello}
	s.mu.Lock()
	s.clients = append(s.clients, client)
	s.mu.Unlock()

	if s.onClient != nil {
		s.onClient(client)
	}
}

// Stop shuts down the server
func (s *Server) Stop() {
	close(s.done)
	s.listener.Close()
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, c := range s.clients {
		c.Conn.Close()
	}
}
```

**Step 4: Run tests**

```bash
go test ./internal/control/ -v -timeout 10s
```
Expected: PASS.

**Step 5: Commit**

```bash
git add server/internal/control/
git commit -m "feat: add TCP control server with handshake protocol"
```

---

## Task 5: UDP Video Streamer

**Files:**
- Create: `server/internal/stream/udp.go`
- Create: `server/internal/stream/udp_test.go`

**Step 1: Write failing tests**

Create `server/internal/stream/udp_test.go`:

```go
package stream

import (
	"bytes"
	"net"
	"testing"
	"time"
)

func TestUDPStreamerSendAndReceive(t *testing.T) {
	// Start receiver
	receiverAddr, err := net.ResolveUDPAddr("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	receiverConn, err := net.ListenUDP("udp", receiverAddr)
	if err != nil {
		t.Fatal(err)
	}
	defer receiverConn.Close()

	// Create streamer targeting receiver
	streamer, err := NewUDPStreamer(receiverConn.LocalAddr().String())
	if err != nil {
		t.Fatalf("failed to create streamer: %v", err)
	}
	defer streamer.Close()

	// Send a small frame
	testData := []byte{0x00, 0x00, 0x00, 0x01, 0x67, 0x42} // fake SPS NAL
	err = streamer.SendFrame(testData, FrameTypeSPS)
	if err != nil {
		t.Fatalf("send error: %v", err)
	}

	// Receive
	buf := make([]byte, 2048)
	receiverConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, _, err := receiverConn.ReadFromUDP(buf)
	if err != nil {
		t.Fatalf("receive error: %v", err)
	}

	pkt, err := UnmarshalPacket(buf[:n])
	if err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if pkt.Header.FrameType != FrameTypeSPS {
		t.Errorf("expected SPS frame type, got %d", pkt.Header.FrameType)
	}
	if !bytes.Equal(pkt.Payload, testData) {
		t.Error("payload mismatch")
	}
}

func TestUDPStreamerLargeFrameFragmentation(t *testing.T) {
	receiverAddr, _ := net.ResolveUDPAddr("udp", "127.0.0.1:0")
	receiverConn, _ := net.ListenUDP("udp", receiverAddr)
	defer receiverConn.Close()

	streamer, _ := NewUDPStreamer(receiverConn.LocalAddr().String())
	defer streamer.Close()

	// Send a large frame that needs fragmentation
	largeData := bytes.Repeat([]byte{0xFF}, MaxPayloadSize*3)
	err := streamer.SendFrame(largeData, FrameTypeIDR)
	if err != nil {
		t.Fatalf("send error: %v", err)
	}

	// Should receive 4 packets (ceil(3*MaxPayloadSize / MaxPayloadSize) = 3... but repeat makes exactly 3x)
	buf := make([]byte, 2048)
	var received []Packet

	for i := 0; i < 3; i++ {
		receiverConn.SetReadDeadline(time.Now().Add(2 * time.Second))
		n, _, err := receiverConn.ReadFromUDP(buf)
		if err != nil {
			t.Fatalf("receive packet %d error: %v", i, err)
		}
		pkt, _ := UnmarshalPacket(buf[:n])
		received = append(received, *pkt)
	}

	if len(received) != 3 {
		t.Fatalf("expected 3 packets, got %d", len(received))
	}

	// Reassemble
	var reassembled []byte
	for _, pkt := range received {
		reassembled = append(reassembled, pkt.Payload...)
	}
	if !bytes.Equal(reassembled, largeData) {
		t.Error("reassembled data mismatch")
	}
}
```

**Step 2: Run tests to verify failure**

```bash
go test ./internal/stream/ -v -timeout 10s
```
Expected: FAIL — NewUDPStreamer not defined.

**Step 3: Implement UDP streamer**

Create `server/internal/stream/udp.go`:

```go
package stream

import (
	"net"
	"sync/atomic"
	"time"
)

// UDPStreamer sends video packets over UDP
type UDPStreamer struct {
	conn     *net.UDPConn
	sequence atomic.Uint32
}

// NewUDPStreamer creates a streamer targeting the given address (host:port)
func NewUDPStreamer(targetAddr string) (*UDPStreamer, error) {
	addr, err := net.ResolveUDPAddr("udp", targetAddr)
	if err != nil {
		return nil, err
	}
	conn, err := net.DialUDP("udp", nil, addr)
	if err != nil {
		return nil, err
	}
	return &UDPStreamer{conn: conn}, nil
}

// SendFrame splits a NAL unit into packets and sends them
func (s *UDPStreamer) SendFrame(nalUnit []byte, frameType byte) error {
	seq := s.sequence.Add(1)
	timestamp := uint64(time.Now().UnixMicro())
	packets := SplitIntoPackets(seq, timestamp, frameType, nalUnit)

	for _, pkt := range packets {
		data := pkt.MarshalPacket()
		if _, err := s.conn.Write(data); err != nil {
			return err
		}
	}
	return nil
}

// Close shuts down the streamer
func (s *UDPStreamer) Close() error {
	return s.conn.Close()
}

// LocalPort returns the local port (useful for tests)
func (s *UDPStreamer) LocalPort() int {
	return s.conn.LocalAddr().(*net.UDPAddr).Port
}
```

**Step 4: Run tests**

```bash
go test ./internal/stream/ -v -timeout 10s
```
Expected: All PASS.

**Step 5: Commit**

```bash
git add server/internal/stream/
git commit -m "feat: add UDP video streamer with packet fragmentation"
```

---

## Task 6: CGo Bridge — Virtual Display

**Files:**
- Create: `server/bridge/display_bridge.h`
- Create: `server/bridge/display_bridge.m`
- Create: `server/internal/display/virtual.go`

> **Note:** This task uses macOS private APIs. Tests are integration-level and require running on a real Mac. No unit test — verified by manual run.

**Step 1: Create Objective-C bridge header**

Create `server/bridge/display_bridge.h`:

```c
#ifndef DISPLAY_BRIDGE_H
#define DISPLAY_BRIDGE_H

#include <CoreGraphics/CoreGraphics.h>

typedef struct {
    int width;
    int height;
    int ppi;
    int hiDPI; // 1 for Retina, 0 for standard
} VirtualDisplayConfig;

typedef struct {
    void *display;       // Opaque pointer to CGVirtualDisplay
    CGDirectDisplayID displayID;
    int success;
    char errorMsg[256];
} VirtualDisplayResult;

VirtualDisplayResult CreateVirtualDisplay(VirtualDisplayConfig config);
void DestroyVirtualDisplay(void *display);
CGDirectDisplayID GetVirtualDisplayID(void *display);

#endif
```

**Step 2: Create Objective-C implementation**

Create `server/bridge/display_bridge.m`:

```objc
#import <Foundation/Foundation.h>
#import <CoreGraphics/CoreGraphics.h>
#import <objc/runtime.h>
#import <objc/message.h>
#include "display_bridge.h"
#include <string.h>

// CGVirtualDisplay is a private API — we use dynamic class loading
// Reference: macos-virtual-display, node-mac-virtual-display

VirtualDisplayResult CreateVirtualDisplay(VirtualDisplayConfig config) {
    VirtualDisplayResult result = {0};

    @autoreleasepool {
        // Load private framework
        NSBundle *bundle = [NSBundle bundleWithPath:@"/System/Library/PrivateFrameworks/CoreDisplay.framework"];
        if (![bundle load]) {
            snprintf(result.errorMsg, sizeof(result.errorMsg), "Failed to load CoreDisplay.framework");
            return result;
        }

        // Get CGVirtualDisplay class
        Class CGVirtualDisplay = NSClassFromString(@"CGVirtualDisplay");
        Class CGVirtualDisplayDescriptor = NSClassFromString(@"CGVirtualDisplayDescriptor");
        Class CGVirtualDisplayMode = NSClassFromString(@"CGVirtualDisplayMode");
        Class CGVirtualDisplaySettings = NSClassFromString(@"CGVirtualDisplaySettings");

        if (!CGVirtualDisplay || !CGVirtualDisplayDescriptor) {
            snprintf(result.errorMsg, sizeof(result.errorMsg), "CGVirtualDisplay classes not found");
            return result;
        }

        // Create descriptor
        id descriptor = [[CGVirtualDisplayDescriptor alloc] init];
        [descriptor setValue:@(20) forKey:@"vendorID"];
        [descriptor setValue:@(0x1234) forKey:@"productID"];
        [descriptor setValue:@"AndroidMac Virtual Display" forKey:@"name"];
        [descriptor setValue:@(config.width) forKey:@"maxPixelsWide"];
        [descriptor setValue:@(config.height) forKey:@"maxPixelsHigh"];

        // Create display mode
        id mode = [[CGVirtualDisplayMode alloc] initWithWidth:config.width
                                                       height:config.height
                                                  refreshRate:60
        ];

        // Create settings
        id settings = [[CGVirtualDisplaySettings alloc] init];
        [settings setValue:@[mode] forKey:@"modes"];
        [settings setValue:@(config.hiDPI) forKey:@"hiDPI"];

        // Apply settings to descriptor
        [descriptor setValue:settings forKey:@"settings"];

        // Create virtual display
        id display = [[CGVirtualDisplay alloc] initWithDescriptor:descriptor];
        if (!display) {
            snprintf(result.errorMsg, sizeof(result.errorMsg), "Failed to create CGVirtualDisplay");
            return result;
        }

        // Get the CGDirectDisplayID
        CGDirectDisplayID displayID = 0;
        NSNumber *dispIDNum = [display valueForKey:@"displayID"];
        if (dispIDNum) {
            displayID = [dispIDNum unsignedIntValue];
        }

        result.display = (__bridge_retained void *)display;
        result.displayID = displayID;
        result.success = 1;
    }

    return result;
}

void DestroyVirtualDisplay(void *display) {
    if (display) {
        @autoreleasepool {
            id obj = (__bridge_transfer id)display;
            obj = nil; // Release
        }
    }
}

CGDirectDisplayID GetVirtualDisplayID(void *display) {
    if (!display) return 0;
    @autoreleasepool {
        id obj = (__bridge id)display;
        NSNumber *dispID = [obj valueForKey:@"displayID"];
        return dispID ? [dispID unsignedIntValue] : 0;
    }
}
```

**Step 3: Create Go wrapper**

Create `server/internal/display/virtual.go`:

```go
package display

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework CoreGraphics -framework Foundation -framework CoreDisplay
#include "../../bridge/display_bridge.h"
*/
import "C"
import (
	"fmt"
	"unsafe"
)

// VirtualDisplay wraps a macOS CGVirtualDisplay
type VirtualDisplay struct {
	ptr       unsafe.Pointer
	displayID uint32
	width     int
	height    int
}

// Config holds virtual display creation parameters
type Config struct {
	Width  int
	Height int
	PPI    int
	HiDPI  bool
}

// New creates a new virtual display with the given configuration
func New(cfg Config) (*VirtualDisplay, error) {
	ccfg := C.VirtualDisplayConfig{
		width:  C.int(cfg.Width),
		height: C.int(cfg.Height),
		ppi:    C.int(cfg.PPI),
	}
	if cfg.HiDPI {
		ccfg.hiDPI = 1
	}

	result := C.CreateVirtualDisplay(ccfg)
	if result.success == 0 {
		return nil, fmt.Errorf("create virtual display: %s", C.GoString(&result.errorMsg[0]))
	}

	return &VirtualDisplay{
		ptr:       result.display,
		displayID: uint32(result.displayID),
		width:     cfg.Width,
		height:    cfg.Height,
	}, nil
}

// DisplayID returns the CGDirectDisplayID for screen capture
func (d *VirtualDisplay) DisplayID() uint32 {
	return d.displayID
}

// Close destroys the virtual display
func (d *VirtualDisplay) Close() {
	if d.ptr != nil {
		C.DestroyVirtualDisplay(d.ptr)
		d.ptr = nil
	}
}
```

**Step 4: Verify compilation**

```bash
cd /Users/luke/sideProject/android-mac/server
go build ./internal/display/
```
Expected: Builds (may show warnings about private API — that's OK).

**Step 5: Commit**

```bash
git add server/bridge/display_bridge.* server/internal/display/
git commit -m "feat: add CGo bridge for CGVirtualDisplay (private API)"
```

---

## Task 7: CGo Bridge — Screen Capture

**Files:**
- Create: `server/bridge/capture_bridge.h`
- Create: `server/bridge/capture_bridge.m`
- Create: `server/internal/capture/capture.go`

**Step 1: Create Objective-C bridge header**

Create `server/bridge/capture_bridge.h`:

```c
#ifndef CAPTURE_BRIDGE_H
#define CAPTURE_BRIDGE_H

#include <CoreGraphics/CoreGraphics.h>

// Callback type: called with each captured frame
// Parameters: pixelBuffer (CVPixelBufferRef), width, height, timestamp (us), userData
typedef void (*FrameCallback)(void *pixelBuffer, int width, int height, int64_t timestamp, void *userData);

typedef struct {
    void *stream;  // Opaque pointer to SCStream
    int success;
    char errorMsg[256];
} CaptureResult;

CaptureResult StartCapture(CGDirectDisplayID displayID, int fps, FrameCallback callback, void *userData);
void StopCapture(void *stream);

#endif
```

**Step 2: Create Objective-C implementation**

Create `server/bridge/capture_bridge.m`:

```objc
#import <Foundation/Foundation.h>
#import <ScreenCaptureKit/ScreenCaptureKit.h>
#include "capture_bridge.h"
#include <string.h>

// Delegate to receive captured frames
@interface FrameHandler : NSObject <SCStreamOutput>
@property (nonatomic, assign) FrameCallback callback;
@property (nonatomic, assign) void *userData;
@end

@implementation FrameHandler

- (void)stream:(SCStream *)stream didOutputSampleBuffer:(CMSampleBufferRef)sampleBuffer ofType:(SCStreamOutputType)type {
    if (type != SCStreamOutputTypeScreen) return;

    CVPixelBufferRef pixelBuffer = CMSampleBufferGetImageBuffer(sampleBuffer);
    if (!pixelBuffer) return;

    int width = (int)CVPixelBufferGetWidth(pixelBuffer);
    int height = (int)CVPixelBufferGetHeight(pixelBuffer);

    CMTime pts = CMSampleBufferGetPresentationTimeStamp(sampleBuffer);
    int64_t timestamp = (int64_t)(CMTimeGetSeconds(pts) * 1000000); // microseconds

    CVPixelBufferRetain(pixelBuffer);
    if (self.callback) {
        self.callback(pixelBuffer, width, height, timestamp, self.userData);
    }
}

@end

CaptureResult StartCapture(CGDirectDisplayID displayID, int fps, FrameCallback callback, void *userData) {
    CaptureResult result = {0};

    @autoreleasepool {
        dispatch_semaphore_t sem = dispatch_semaphore_create(0);
        __block SCDisplay *targetDisplay = nil;

        [SCShareableContent getShareableContentWithCompletionHandler:^(SCShareableContent *content, NSError *error) {
            if (error) {
                snprintf(result.errorMsg, sizeof(result.errorMsg), "getShareableContent: %s",
                         [[error localizedDescription] UTF8String]);
                dispatch_semaphore_signal(sem);
                return;
            }

            for (SCDisplay *display in content.displays) {
                if (display.displayID == displayID) {
                    targetDisplay = display;
                    break;
                }
            }
            dispatch_semaphore_signal(sem);
        }];

        dispatch_semaphore_wait(sem, dispatch_time(DISPATCH_TIME_NOW, 5 * NSEC_PER_SEC));

        if (!targetDisplay) {
            if (result.errorMsg[0] == '\0') {
                snprintf(result.errorMsg, sizeof(result.errorMsg), "Display 0x%x not found", displayID);
            }
            return result;
        }

        // Create content filter for this display
        SCContentFilter *filter = [[SCContentFilter alloc] initWithDisplay:targetDisplay excludingWindows:@[]];

        // Configure stream
        SCStreamConfiguration *config = [[SCStreamConfiguration alloc] init];
        config.width = targetDisplay.width;
        config.height = targetDisplay.height;
        config.minimumFrameInterval = CMTimeMake(1, fps);
        config.pixelFormat = kCVPixelFormatType_32BGRA;
        config.showsCursor = YES;

        // Create stream
        NSError *error = nil;
        SCStream *stream = [[SCStream alloc] initWithFilter:filter configuration:config delegate:nil];

        // Create and add frame handler
        FrameHandler *handler = [[FrameHandler alloc] init];
        handler.callback = callback;
        handler.userData = userData;

        [stream addStreamOutput:handler type:SCStreamOutputTypeScreen sampleHandlerQueue:dispatch_get_global_queue(QOS_CLASS_USER_INTERACTIVE, 0) error:&error];
        if (error) {
            snprintf(result.errorMsg, sizeof(result.errorMsg), "addStreamOutput: %s",
                     [[error localizedDescription] UTF8String]);
            return result;
        }

        // Start capture
        dispatch_semaphore_t startSem = dispatch_semaphore_create(0);
        __block NSError *startError = nil;

        [stream startCaptureWithCompletionHandler:^(NSError *err) {
            startError = err;
            dispatch_semaphore_signal(startSem);
        }];

        dispatch_semaphore_wait(startSem, dispatch_time(DISPATCH_TIME_NOW, 5 * NSEC_PER_SEC));

        if (startError) {
            snprintf(result.errorMsg, sizeof(result.errorMsg), "startCapture: %s",
                     [[startError localizedDescription] UTF8String]);
            return result;
        }

        result.stream = (__bridge_retained void *)stream;
        // Also retain handler to prevent deallocation
        (void)(__bridge_retained void *)handler;
        result.success = 1;
    }

    return result;
}

void StopCapture(void *stream) {
    if (stream) {
        @autoreleasepool {
            SCStream *s = (__bridge_transfer SCStream *)stream;
            dispatch_semaphore_t sem = dispatch_semaphore_create(0);
            [s stopCaptureWithCompletionHandler:^(NSError *error) {
                dispatch_semaphore_signal(sem);
            }];
            dispatch_semaphore_wait(sem, dispatch_time(DISPATCH_TIME_NOW, 3 * NSEC_PER_SEC));
        }
    }
}
```

**Step 3: Create Go wrapper**

Create `server/internal/capture/capture.go`:

```go
package capture

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework ScreenCaptureKit -framework CoreGraphics -framework CoreMedia -framework CoreVideo -framework Foundation
#include "../../bridge/capture_bridge.h"

extern void goFrameCallback(void *pixelBuffer, int width, int height, int64_t timestamp, void *userData);
*/
import "C"
import (
	"fmt"
	"sync"
	"unsafe"
)

// Frame represents a captured screen frame
type Frame struct {
	PixelBuffer unsafe.Pointer // CVPixelBufferRef — must be released after use
	Width       int
	Height      int
	Timestamp   int64 // microseconds
}

// FrameHandler is called for each captured frame
type FrameHandler func(Frame)

var (
	handlerMu sync.Mutex
	handlers  = make(map[uintptr]FrameHandler)
	nextID    uintptr
)

func registerHandler(fn FrameHandler) uintptr {
	handlerMu.Lock()
	defer handlerMu.Unlock()
	nextID++
	handlers[nextID] = fn
	return nextID
}

func unregisterHandler(id uintptr) {
	handlerMu.Lock()
	defer handlerMu.Unlock()
	delete(handlers, id)
}

//export goFrameCallback
func goFrameCallback(pixelBuffer unsafe.Pointer, width C.int, height C.int, timestamp C.int64_t, userData unsafe.Pointer) {
	id := uintptr(userData)
	handlerMu.Lock()
	fn, ok := handlers[id]
	handlerMu.Unlock()
	if ok {
		fn(Frame{
			PixelBuffer: pixelBuffer,
			Width:       int(width),
			Height:      int(height),
			Timestamp:   int64(timestamp),
		})
	}
}

// Capturer captures frames from a specific display
type Capturer struct {
	stream    unsafe.Pointer
	handlerID uintptr
}

// Start begins capturing frames from the display with given ID
func Start(displayID uint32, fps int, handler FrameHandler) (*Capturer, error) {
	id := registerHandler(handler)

	result := C.StartCapture(
		C.CGDirectDisplayID(displayID),
		C.int(fps),
		(C.FrameCallback)(C.goFrameCallback),
		unsafe.Pointer(id),
	)

	if result.success == 0 {
		unregisterHandler(id)
		return nil, fmt.Errorf("start capture: %s", C.GoString(&result.errorMsg[0]))
	}

	return &Capturer{
		stream:    result.stream,
		handlerID: id,
	}, nil
}

// Stop ends the capture session
func (c *Capturer) Stop() {
	if c.stream != nil {
		C.StopCapture(c.stream)
		c.stream = nil
	}
	unregisterHandler(c.handlerID)
}

// ReleasePixelBuffer releases a CVPixelBuffer after processing
func ReleasePixelBuffer(buf unsafe.Pointer) {
	if buf != nil {
		C.CVPixelBufferRelease(C.CVPixelBufferRef(buf))
	}
}
```

**Step 4: Verify compilation**

```bash
cd /Users/luke/sideProject/android-mac/server
go build ./internal/capture/
```
Expected: Builds without errors on macOS 12.3+.

**Step 5: Commit**

```bash
git add server/bridge/capture_bridge.* server/internal/capture/
git commit -m "feat: add CGo bridge for ScreenCaptureKit frame capture"
```

---

## Task 8: CGo Bridge — H.264 Encoder

**Files:**
- Create: `server/bridge/encoder_bridge.h`
- Create: `server/bridge/encoder_bridge.m`
- Create: `server/internal/encoder/h264.go`

**Step 1: Create Objective-C bridge header**

Create `server/bridge/encoder_bridge.h`:

```c
#ifndef ENCODER_BRIDGE_H
#define ENCODER_BRIDGE_H

#include <CoreVideo/CoreVideo.h>

// Callback: called with each encoded NAL unit
// Parameters: nalData, nalSize, isKeyframe, timestamp (us), userData
typedef void (*EncodedCallback)(const uint8_t *nalData, int nalSize, int isKeyframe, int64_t timestamp, void *userData);

typedef struct {
    void *session;  // VTCompressionSessionRef
    int success;
    char errorMsg[256];
} EncoderResult;

EncoderResult CreateEncoder(int width, int height, int fps, int bitrate, EncodedCallback callback, void *userData);
int EncodeFrame(void *session, CVPixelBufferRef pixelBuffer, int64_t timestamp);
void DestroyEncoder(void *session);

#endif
```

**Step 2: Create Objective-C implementation**

Create `server/bridge/encoder_bridge.m`:

```objc
#import <Foundation/Foundation.h>
#import <VideoToolbox/VideoToolbox.h>
#include "encoder_bridge.h"
#include <string.h>

typedef struct {
    EncodedCallback callback;
    void *userData;
} EncoderContext;

// Convert AVCC format (length-prefixed) to Annex-B (start code prefixed)
static void outputCallback(void *outputCallbackRefCon,
                           void *sourceFrameRefCon,
                           OSStatus status,
                           VTEncodeInfoFlags infoFlags,
                           CMSampleBufferRef sampleBuffer) {
    if (status != noErr || !sampleBuffer) return;

    EncoderContext *ctx = (EncoderContext *)outputCallbackRefCon;

    // Check if keyframe
    CFArrayRef attachments = CMSampleBufferGetSampleAttachmentsArray(sampleBuffer, false);
    BOOL isKeyframe = NO;
    if (attachments && CFArrayGetCount(attachments) > 0) {
        CFDictionaryRef dict = CFArrayGetValueAtIndex(attachments, 0);
        isKeyframe = !CFDictionaryContainsKey(dict, kCMSampleAttachmentKey_NotSync);
    }

    // Get timestamp
    CMTime pts = CMSampleBufferGetPresentationTimeStamp(sampleBuffer);
    int64_t timestamp = (int64_t)(CMTimeGetSeconds(pts) * 1000000);

    // If keyframe, extract SPS/PPS first
    if (isKeyframe) {
        CMFormatDescriptionRef format = CMSampleBufferGetFormatDescription(sampleBuffer);

        // SPS
        size_t spsSize = 0;
        const uint8_t *sps = NULL;
        CMVideoFormatDescriptionGetH264ParameterSetAtIndex(format, 0, &sps, &spsSize, NULL, NULL);
        if (sps && spsSize > 0) {
            // Send with Annex-B start code
            uint8_t *spsNAL = malloc(4 + spsSize);
            spsNAL[0] = 0; spsNAL[1] = 0; spsNAL[2] = 0; spsNAL[3] = 1;
            memcpy(spsNAL + 4, sps, spsSize);
            ctx->callback(spsNAL, (int)(4 + spsSize), 1, timestamp, ctx->userData);
            free(spsNAL);
        }

        // PPS
        size_t ppsSize = 0;
        const uint8_t *pps = NULL;
        CMVideoFormatDescriptionGetH264ParameterSetAtIndex(format, 1, &pps, &ppsSize, NULL, NULL);
        if (pps && ppsSize > 0) {
            uint8_t *ppsNAL = malloc(4 + ppsSize);
            ppsNAL[0] = 0; ppsNAL[1] = 0; ppsNAL[2] = 0; ppsNAL[3] = 1;
            memcpy(ppsNAL + 4, pps, ppsSize);
            ctx->callback(ppsNAL, (int)(4 + ppsSize), 1, timestamp, ctx->userData);
            free(ppsNAL);
        }
    }

    // Get encoded data block
    CMBlockBufferRef dataBuffer = CMSampleBufferGetDataBuffer(sampleBuffer);
    size_t totalLength = 0;
    char *dataPtr = NULL;
    CMBlockBufferGetDataPointer(dataBuffer, 0, NULL, &totalLength, &dataPtr);

    // Convert AVCC (length-prefixed) to Annex-B (start code)
    size_t offset = 0;
    while (offset < totalLength) {
        uint32_t naluLength = 0;
        memcpy(&naluLength, dataPtr + offset, 4);
        naluLength = CFSwapInt32BigToHost(naluLength);
        offset += 4;

        uint8_t *nalUnit = malloc(4 + naluLength);
        nalUnit[0] = 0; nalUnit[1] = 0; nalUnit[2] = 0; nalUnit[3] = 1;
        memcpy(nalUnit + 4, dataPtr + offset, naluLength);

        ctx->callback(nalUnit, (int)(4 + naluLength), isKeyframe ? 1 : 0, timestamp, ctx->userData);
        free(nalUnit);

        offset += naluLength;
    }
}

EncoderResult CreateEncoder(int width, int height, int fps, int bitrate, EncodedCallback callback, void *userData) {
    EncoderResult result = {0};

    EncoderContext *ctx = malloc(sizeof(EncoderContext));
    ctx->callback = callback;
    ctx->userData = userData;

    VTCompressionSessionRef session = NULL;
    OSStatus status = VTCompressionSessionCreate(
        NULL,           // allocator
        width,
        height,
        kCMVideoCodecType_H264,
        NULL,           // encoder specification
        NULL,           // source image buffer attributes
        NULL,           // compressed data allocator
        outputCallback,
        ctx,            // callback ref con
        &session
    );

    if (status != noErr) {
        snprintf(result.errorMsg, sizeof(result.errorMsg), "VTCompressionSessionCreate failed: %d", (int)status);
        free(ctx);
        return result;
    }

    // Configure for real-time low latency
    VTSessionSetProperty(session, kVTCompressionPropertyKey_RealTime, kCFBooleanTrue);
    VTSessionSetProperty(session, kVTCompressionPropertyKey_ProfileLevel, kVTProfileLevel_H264_Main_AutoLevel);
    VTSessionSetProperty(session, kVTCompressionPropertyKey_AllowFrameReordering, kCFBooleanFalse); // No B-frames for low latency

    // Bitrate
    CFNumberRef bitrateRef = CFNumberCreate(NULL, kCFNumberIntType, &bitrate);
    VTSessionSetProperty(session, kVTCompressionPropertyKey_AverageBitRate, bitrateRef);
    CFRelease(bitrateRef);

    // Max keyframe interval
    int keyframeInterval = fps * 2; // Every 2 seconds
    CFNumberRef kfRef = CFNumberCreate(NULL, kCFNumberIntType, &keyframeInterval);
    VTSessionSetProperty(session, kVTCompressionPropertyKey_MaxKeyFrameInterval, kfRef);
    CFRelease(kfRef);

    // Expected frame rate
    CFNumberRef fpsRef = CFNumberCreate(NULL, kCFNumberIntType, &fps);
    VTSessionSetProperty(session, kVTCompressionPropertyKey_ExpectedFrameRate, fpsRef);
    CFRelease(fpsRef);

    VTCompressionSessionPrepareToEncodeFrames(session);

    result.session = session;
    result.success = 1;
    return result;
}

int EncodeFrame(void *session, CVPixelBufferRef pixelBuffer, int64_t timestamp) {
    CMTime pts = CMTimeMake(timestamp, 1000000); // microseconds
    OSStatus status = VTCompressionSessionEncodeFrame(
        (VTCompressionSessionRef)session,
        pixelBuffer,
        pts,
        kCMTimeInvalid,  // duration
        NULL,            // frame properties
        NULL,            // source frame ref con
        NULL             // info flags out
    );
    return (int)status;
}

void DestroyEncoder(void *session) {
    if (session) {
        VTCompressionSessionCompleteFrames((VTCompressionSessionRef)session, kCMTimeInvalid);
        VTCompressionSessionInvalidate((VTCompressionSessionRef)session);
        CFRelease(session);
        // Note: EncoderContext leak — in production, track and free
    }
}
```

**Step 3: Create Go wrapper**

Create `server/internal/encoder/h264.go`:

```go
package encoder

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework VideoToolbox -framework CoreMedia -framework CoreVideo -framework CoreFoundation -framework Foundation
#include "../../bridge/encoder_bridge.h"

extern void goEncodedCallback(const uint8_t *nalData, int nalSize, int isKeyframe, int64_t timestamp, void *userData);
*/
import "C"
import (
	"sync"
	"unsafe"
)

// NALUnit represents an encoded H.264 NAL unit
type NALUnit struct {
	Data       []byte
	IsKeyframe bool
	Timestamp  int64 // microseconds
}

// NALHandler is called for each encoded NAL unit
type NALHandler func(NALUnit)

var (
	nalHandlerMu sync.Mutex
	nalHandlers  = make(map[uintptr]NALHandler)
	nalNextID    uintptr
)

func registerNALHandler(fn NALHandler) uintptr {
	nalHandlerMu.Lock()
	defer nalHandlerMu.Unlock()
	nalNextID++
	nalHandlers[nalNextID] = fn
	return nalNextID
}

func unregisterNALHandler(id uintptr) {
	nalHandlerMu.Lock()
	defer nalHandlerMu.Unlock()
	delete(nalHandlers, id)
}

//export goEncodedCallback
func goEncodedCallback(nalData *C.uint8_t, nalSize C.int, isKeyframe C.int, timestamp C.int64_t, userData unsafe.Pointer) {
	id := uintptr(userData)
	nalHandlerMu.Lock()
	fn, ok := nalHandlers[id]
	nalHandlerMu.Unlock()
	if !ok {
		return
	}

	// Copy NAL data to Go-managed memory
	size := int(nalSize)
	goData := make([]byte, size)
	C.memcpy(unsafe.Pointer(&goData[0]), unsafe.Pointer(nalData), C.size_t(size))

	fn(NALUnit{
		Data:       goData,
		IsKeyframe: isKeyframe != 0,
		Timestamp:  int64(timestamp),
	})
}

// Encoder wraps a VideoToolbox H.264 encoding session
type Encoder struct {
	session   unsafe.Pointer
	handlerID uintptr
}

// Config holds encoder parameters
type Config struct {
	Width   int
	Height  int
	FPS     int
	Bitrate int // bits per second
}

// New creates a new H.264 encoder
func New(cfg Config, handler NALHandler) (*Encoder, error) {
	id := registerNALHandler(handler)

	result := C.CreateEncoder(
		C.int(cfg.Width),
		C.int(cfg.Height),
		C.int(cfg.FPS),
		C.int(cfg.Bitrate),
		(C.EncodedCallback)(C.goEncodedCallback),
		unsafe.Pointer(id),
	)

	if result.success == 0 {
		unregisterNALHandler(id)
		return nil, fmt.Errorf("create encoder: %s", C.GoString(&result.errorMsg[0]))
	}

	return &Encoder{
		session:   result.session,
		handlerID: id,
	}, nil
}

// Encode submits a frame (CVPixelBufferRef) for encoding
func (e *Encoder) Encode(pixelBuffer unsafe.Pointer, timestamp int64) error {
	status := C.EncodeFrame(e.session, C.CVPixelBufferRef(pixelBuffer), C.int64_t(timestamp))
	if status != 0 {
		return fmt.Errorf("encode frame: status %d", status)
	}
	return nil
}

// Close destroys the encoder session
func (e *Encoder) Close() {
	if e.session != nil {
		C.DestroyEncoder(e.session)
		e.session = nil
	}
	unregisterNALHandler(e.handlerID)
}
```

Add missing import at top:

```go
import (
	"fmt"
	"sync"
	"unsafe"
)
```

**Step 4: Verify compilation**

```bash
cd /Users/luke/sideProject/android-mac/server
go build ./internal/encoder/
```
Expected: Builds without errors.

**Step 5: Commit**

```bash
git add server/bridge/encoder_bridge.* server/internal/encoder/
git commit -m "feat: add CGo bridge for VideoToolbox H.264 encoding"
```

---

## Task 9: Mac Server Main — Wire Everything Together

**Files:**
- Modify: `server/cmd/server/main.go`

**Step 1: Implement the full server pipeline**

Update `server/cmd/server/main.go`:

```go
package main

import (
	"fmt"
	"log"
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
	controlPort = 9000
	streamPort  = 9001
	defaultFPS  = 60
	defaultBitrate = 8_000_000 // 8 Mbps
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

	// 3. When a client connects, start the pipeline
	ctrlServer.OnClient(func(client control.ClientConn) {
		log.Printf("client connected: %s", client.Hello.Device)

		w := client.Hello.Screen.Width
		h := client.Hello.Screen.Height
		remoteAddr := client.Conn.RemoteAddr().String()
		// Extract IP, use streamPort
		host, _, _ := net.SplitHostPort(remoteAddr)
		targetAddr := fmt.Sprintf("%s:%d", host, streamPort)

		go startPipeline(w, h, targetAddr)
	})

	go ctrlServer.AcceptLoop()
	log.Printf("control server listening on port %d", controlPort)

	// Wait for shutdown
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	log.Println("shutting down...")
}

func startPipeline(width, height int, targetAddr string) {
	// 1. Create virtual display
	vd, err := display.New(display.Config{
		Width:  width,
		Height: height,
		PPI:    224,
		HiDPI:  false,
	})
	if err != nil {
		log.Printf("virtual display failed: %v", err)
		return
	}
	defer vd.Close()
	log.Printf("virtual display created: %dx%d (ID: 0x%x)", width, height, vd.DisplayID())

	// 2. Create UDP streamer
	streamer, err := stream.NewUDPStreamer(targetAddr)
	if err != nil {
		log.Printf("UDP streamer failed: %v", err)
		return
	}
	defer streamer.Close()
	log.Printf("UDP streaming to %s", targetAddr)

	// 3. Create H.264 encoder → feeds NAL units to UDP streamer
	enc, err := encoder.New(encoder.Config{
		Width:   width,
		Height:  height,
		FPS:     defaultFPS,
		Bitrate: defaultBitrate,
	}, func(nal encoder.NALUnit) {
		frameType := stream.FrameTypeP
		if nal.IsKeyframe {
			frameType = stream.FrameTypeIDR
		}
		if err := streamer.SendFrame(nal.Data, byte(frameType)); err != nil {
			log.Printf("stream send error: %v", err)
		}
	})
	if err != nil {
		log.Printf("encoder failed: %v", err)
		return
	}
	defer enc.Close()
	log.Println("H.264 encoder ready")

	// 4. Start screen capture → feeds frames to encoder
	cap, err := capture.Start(vd.DisplayID(), defaultFPS, func(frame capture.Frame) {
		defer capture.ReleasePixelBuffer(frame.PixelBuffer)
		if err := enc.Encode(frame.PixelBuffer, frame.Timestamp); err != nil {
			log.Printf("encode error: %v", err)
		}
	})
	if err != nil {
		log.Printf("capture failed: %v", err)
		return
	}
	defer cap.Stop()
	log.Println("screen capture started — pipeline active")

	// Keep running until interrupted
	select {}
}
```

Add missing import `"net"` to imports.

**Step 2: Verify compilation**

```bash
cd /Users/luke/sideProject/android-mac/server
go build ./cmd/server/
```
Expected: Builds without errors.

**Step 3: Commit**

```bash
git add server/cmd/server/main.go
git commit -m "feat: wire up Mac server pipeline (display→capture→encode→stream)"
```

---

## Task 10: Android Project Setup

**Files:**
- Create: Android project with Gradle, manifest, and basic activity

> **Note:** This task creates the full Android project scaffold. Use Android Studio or create files manually.

**Step 1: Create project structure**

```bash
mkdir -p client/app/src/main/java/com/androidmac/client/{discovery,control,video,protocol}
mkdir -p client/app/src/main/res/{layout,values}
```

**Step 2: Create settings.gradle.kts**

Create `client/settings.gradle.kts`:

```kotlin
pluginManagement {
    repositories {
        google()
        mavenCentral()
        gradlePluginPortal()
    }
}
dependencyResolution {
    repositories {
        google()
        mavenCentral()
    }
}
rootProject.name = "AndroidMacClient"
include(":app")
```

**Step 3: Create root build.gradle.kts**

Create `client/build.gradle.kts`:

```kotlin
plugins {
    id("com.android.application") version "8.2.0" apply false
    id("org.jetbrains.kotlin.android") version "1.9.22" apply false
}
```

**Step 4: Create app/build.gradle.kts**

Create `client/app/build.gradle.kts`:

```kotlin
plugins {
    id("com.android.application")
    id("org.jetbrains.kotlin.android")
}

android {
    namespace = "com.androidmac.client"
    compileSdk = 34

    defaultConfig {
        applicationId = "com.androidmac.client"
        minSdk = 29  // Android 10 — Tab S6 Lite 2020
        targetSdk = 34
        versionCode = 1
        versionName = "1.0"
    }

    buildTypes {
        release {
            isMinifyEnabled = false
        }
    }

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_1_8
        targetCompatibility = JavaVersion.VERSION_1_8
    }

    kotlinOptions {
        jvmTarget = "1.8"
    }
}

dependencies {
    implementation("androidx.core:core-ktx:1.12.0")
    implementation("androidx.appcompat:appcompat:1.6.1")
    implementation("org.jetbrains.kotlinx:kotlinx-coroutines-android:1.7.3")
    implementation("androidx.lifecycle:lifecycle-runtime-ktx:2.7.0")
    implementation("com.google.android.material:material:1.11.0")
}
```

**Step 5: Create gradle.properties**

Create `client/gradle.properties`:

```properties
org.gradle.jvmargs=-Xmx2048m
android.useAndroidX=true
kotlin.code.style=official
```

**Step 6: Create AndroidManifest.xml**

Create `client/app/src/main/AndroidManifest.xml`:

```xml
<?xml version="1.0" encoding="utf-8"?>
<manifest xmlns:android="http://schemas.android.com/apk/res/android">

    <uses-permission android:name="android.permission.INTERNET" />
    <uses-permission android:name="android.permission.ACCESS_NETWORK_STATE" />
    <uses-permission android:name="android.permission.ACCESS_WIFI_STATE" />

    <application
        android:allowBackup="false"
        android:label="AndroidMac"
        android:supportsRtl="true"
        android:theme="@style/Theme.AppCompat.NoActionBar">

        <activity
            android:name=".MainActivity"
            android:exported="true"
            android:screenOrientation="landscape"
            android:configChanges="orientation|screenSize|keyboardHidden">
            <intent-filter>
                <action android:name="android.intent.action.MAIN" />
                <category android:name="android.intent.category.LAUNCHER" />
            </intent-filter>
        </activity>

        <activity
            android:name=".DisplayActivity"
            android:exported="false"
            android:screenOrientation="landscape"
            android:configChanges="orientation|screenSize|keyboardHidden"
            android:theme="@style/Theme.AppCompat.NoActionBar" />
    </application>
</manifest>
```

**Step 7: Create protocol messages**

Create `client/app/src/main/java/com/androidmac/client/protocol/Messages.kt`:

```kotlin
package com.androidmac.client.protocol

import org.json.JSONArray
import org.json.JSONObject

data class ScreenInfo(val width: Int, val height: Int, val dpi: Int)

data class ClientHello(
    val device: String,
    val screen: ScreenInfo,
    val capabilities: List<String>,
    val codecs: List<String>
) {
    fun toJson(): String {
        val obj = JSONObject()
        obj.put("device", device)
        obj.put("screen", JSONObject().apply {
            put("width", screen.width)
            put("height", screen.height)
            put("dpi", screen.dpi)
        })
        obj.put("capabilities", JSONArray(capabilities))
        obj.put("codecs", JSONArray(codecs))
        return obj.toString()
    }
}

data class DisplayInfo(val width: Int, val height: Int)

data class ServerHello(
    val virtualDisplay: DisplayInfo,
    val codec: String,
    val bitrate: Int,
    val fps: Int,
    val streamPort: Int
) {
    companion object {
        fun fromJson(json: String): ServerHello {
            val obj = JSONObject(json)
            val disp = obj.getJSONObject("virtualDisplay")
            return ServerHello(
                virtualDisplay = DisplayInfo(disp.getInt("width"), disp.getInt("height")),
                codec = obj.getString("codec"),
                bitrate = obj.getInt("bitrate"),
                fps = obj.getInt("fps"),
                streamPort = obj.getInt("streamPort")
            )
        }
    }
}
```

**Step 8: Create basic layouts**

Create `client/app/src/main/res/layout/activity_main.xml`:

```xml
<?xml version="1.0" encoding="utf-8"?>
<LinearLayout xmlns:android="http://schemas.android.com/apk/res/android"
    android:layout_width="match_parent"
    android:layout_height="match_parent"
    android:orientation="vertical"
    android:gravity="center"
    android:padding="32dp">

    <TextView
        android:id="@+id/statusText"
        android:layout_width="wrap_content"
        android:layout_height="wrap_content"
        android:text="Scanning for Mac..."
        android:textSize="18sp" />

    <androidx.recyclerview.widget.RecyclerView
        android:id="@+id/deviceList"
        android:layout_width="match_parent"
        android:layout_height="0dp"
        android:layout_weight="1"
        android:layout_marginTop="16dp" />

    <com.google.android.material.textfield.TextInputLayout
        android:layout_width="match_parent"
        android:layout_height="wrap_content"
        android:layout_marginTop="16dp">

        <com.google.android.material.textfield.TextInputEditText
            android:id="@+id/manualIpInput"
            android:layout_width="match_parent"
            android:layout_height="wrap_content"
            android:hint="Manual IP (e.g. 192.168.1.100)"
            android:inputType="text" />
    </com.google.android.material.textfield.TextInputLayout>

    <Button
        android:id="@+id/connectButton"
        android:layout_width="wrap_content"
        android:layout_height="wrap_content"
        android:layout_marginTop="8dp"
        android:text="Connect" />
</LinearLayout>
```

Create `client/app/src/main/res/layout/activity_display.xml`:

```xml
<?xml version="1.0" encoding="utf-8"?>
<FrameLayout xmlns:android="http://schemas.android.com/apk/res/android"
    android:layout_width="match_parent"
    android:layout_height="match_parent"
    android:background="#000000">

    <SurfaceView
        android:id="@+id/surfaceView"
        android:layout_width="match_parent"
        android:layout_height="match_parent" />
</FrameLayout>
```

Create `client/app/src/main/res/values/strings.xml`:

```xml
<resources>
    <string name="app_name">AndroidMac</string>
</resources>
```

**Step 9: Commit**

```bash
git add client/
git commit -m "feat: scaffold Android client project with Gradle, manifest, protocol, layouts"
```

---

## Task 11: Android NSD Discovery

**Files:**
- Create: `client/app/src/main/java/com/androidmac/client/discovery/NsdDiscovery.kt`

**Step 1: Implement NSD discovery**

Create `client/app/src/main/java/com/androidmac/client/discovery/NsdDiscovery.kt`:

```kotlin
package com.androidmac.client.discovery

import android.content.Context
import android.net.nsd.NsdManager
import android.net.nsd.NsdServiceInfo
import android.util.Log
import kotlinx.coroutines.channels.awaitClose
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.callbackFlow

data class DiscoveredServer(
    val name: String,
    val host: String,
    val port: Int
)

class NsdDiscovery(context: Context) {
    private val nsdManager = context.getSystemService(Context.NSD_SERVICE) as NsdManager

    companion object {
        private const val TAG = "NsdDiscovery"
        private const val SERVICE_TYPE = "_androidmac._tcp."
    }

    fun discover(): Flow<DiscoveredServer> = callbackFlow {
        val resolveListener = object : NsdManager.ResolveListener {
            override fun onResolveFailed(serviceInfo: NsdServiceInfo, errorCode: Int) {
                Log.e(TAG, "Resolve failed: $errorCode")
            }

            override fun onServiceResolved(serviceInfo: NsdServiceInfo) {
                val server = DiscoveredServer(
                    name = serviceInfo.serviceName,
                    host = serviceInfo.host.hostAddress ?: "",
                    port = serviceInfo.port
                )
                Log.d(TAG, "Resolved: $server")
                trySend(server)
            }
        }

        val discoveryListener = object : NsdManager.DiscoveryListener {
            override fun onDiscoveryStarted(serviceType: String) {
                Log.d(TAG, "Discovery started")
            }

            override fun onServiceFound(serviceInfo: NsdServiceInfo) {
                Log.d(TAG, "Found: ${serviceInfo.serviceName}")
                nsdManager.resolveService(serviceInfo, resolveListener)
            }

            override fun onServiceLost(serviceInfo: NsdServiceInfo) {
                Log.d(TAG, "Lost: ${serviceInfo.serviceName}")
            }

            override fun onDiscoveryStopped(serviceType: String) {
                Log.d(TAG, "Discovery stopped")
            }

            override fun onStartDiscoveryFailed(serviceType: String, errorCode: Int) {
                Log.e(TAG, "Start discovery failed: $errorCode")
            }

            override fun onStopDiscoveryFailed(serviceType: String, errorCode: Int) {
                Log.e(TAG, "Stop discovery failed: $errorCode")
            }
        }

        nsdManager.discoverServices(SERVICE_TYPE, NsdManager.PROTOCOL_DNS_SD, discoveryListener)

        awaitClose {
            try {
                nsdManager.stopServiceDiscovery(discoveryListener)
            } catch (e: Exception) {
                Log.w(TAG, "Stop discovery error: ${e.message}")
            }
        }
    }
}
```

**Step 2: Commit**

```bash
git add client/app/src/main/java/com/androidmac/client/discovery/
git commit -m "feat: add Android NSD service discovery with Kotlin Flow"
```

---

## Task 12: Android Control Client (TCP Handshake)

**Files:**
- Create: `client/app/src/main/java/com/androidmac/client/control/ControlClient.kt`

**Step 1: Implement control client**

Create `client/app/src/main/java/com/androidmac/client/control/ControlClient.kt`:

```kotlin
package com.androidmac.client.control

import android.util.DisplayMetrics
import com.androidmac.client.protocol.ClientHello
import com.androidmac.client.protocol.ScreenInfo
import com.androidmac.client.protocol.ServerHello
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import java.io.BufferedReader
import java.io.InputStreamReader
import java.io.PrintWriter
import java.net.Socket

class ControlClient {
    private var socket: Socket? = null
    private var writer: PrintWriter? = null
    private var reader: BufferedReader? = null

    suspend fun connect(host: String, port: Int, metrics: DisplayMetrics): ServerHello =
        withContext(Dispatchers.IO) {
            val sock = Socket(host, port)
            sock.tcpNoDelay = true
            socket = sock

            writer = PrintWriter(sock.getOutputStream(), true)
            reader = BufferedReader(InputStreamReader(sock.getInputStream()))

            // Send ClientHello
            val hello = ClientHello(
                device = android.os.Build.MODEL,
                screen = ScreenInfo(
                    width = metrics.widthPixels,
                    height = metrics.heightPixels,
                    dpi = metrics.densityDpi
                ),
                capabilities = listOf("touch", "pen", "pressure"),
                codecs = listOf("h264")
            )

            writer!!.println(hello.toJson())

            // Read ServerHello
            val responseLine = reader!!.readLine()
                ?: throw Exception("Server closed connection during handshake")

            ServerHello.fromJson(responseLine)
        }

    fun disconnect() {
        try {
            socket?.close()
        } catch (_: Exception) {}
        socket = null
        writer = null
        reader = null
    }
}
```

**Step 2: Commit**

```bash
git add client/app/src/main/java/com/androidmac/client/control/
git commit -m "feat: add Android TCP control client with handshake"
```

---

## Task 13: Android UDP Receiver + Video Decoder

**Files:**
- Create: `client/app/src/main/java/com/androidmac/client/video/UdpReceiver.kt`
- Create: `client/app/src/main/java/com/androidmac/client/video/VideoDecoder.kt`

**Step 1: Implement UDP receiver**

Create `client/app/src/main/java/com/androidmac/client/video/UdpReceiver.kt`:

```kotlin
package com.androidmac.client.video

import android.util.Log
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.isActive
import kotlinx.coroutines.withContext
import java.net.DatagramPacket
import java.net.DatagramSocket
import java.nio.ByteBuffer
import java.nio.ByteOrder

data class VideoPacket(
    val sequence: Long,
    val timestamp: Long,
    val frameType: Byte,
    val fragIndex: Int,
    val fragTotal: Int,
    val payload: ByteArray
)

class UdpReceiver(private val port: Int) {
    private var socket: DatagramSocket? = null

    companion object {
        private const val TAG = "UdpReceiver"
        private const val HEADER_SIZE = 15
        private const val MAX_PACKET_SIZE = 1500
    }

    suspend fun receiveLoop(onPacket: (VideoPacket) -> Unit) = withContext(Dispatchers.IO) {
        val sock = DatagramSocket(port)
        sock.receiveBufferSize = 2 * 1024 * 1024 // 2MB buffer
        socket = sock
        Log.d(TAG, "Listening on UDP port $port")

        val buf = ByteArray(MAX_PACKET_SIZE)
        val packet = DatagramPacket(buf, buf.size)

        while (isActive) {
            try {
                sock.receive(packet)
                if (packet.length < HEADER_SIZE) continue

                val bb = ByteBuffer.wrap(buf, 0, packet.length).order(ByteOrder.BIG_ENDIAN)
                val seq = bb.int.toLong() and 0xFFFFFFFFL
                val ts = bb.long
                val frameType = bb.get()
                val fragIndex = bb.get().toInt() and 0xFF
                val fragTotal = bb.get().toInt() and 0xFF

                val payloadSize = packet.length - HEADER_SIZE
                val payload = ByteArray(payloadSize)
                System.arraycopy(buf, HEADER_SIZE, payload, 0, payloadSize)

                onPacket(VideoPacket(seq, ts, frameType, fragIndex, fragTotal, payload))
            } catch (e: Exception) {
                if (isActive) Log.e(TAG, "Receive error: ${e.message}")
            }
        }
    }

    fun close() {
        socket?.close()
        socket = null
    }
}
```

**Step 2: Implement video decoder**

Create `client/app/src/main/java/com/androidmac/client/video/VideoDecoder.kt`:

```kotlin
package com.androidmac.client.video

import android.media.MediaCodec
import android.media.MediaFormat
import android.util.Log
import android.view.Surface
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.isActive
import kotlinx.coroutines.withContext
import java.util.concurrent.ConcurrentLinkedQueue

class VideoDecoder(
    private val width: Int,
    private val height: Int,
    private val surface: Surface
) {
    private var codec: MediaCodec? = null
    private val nalQueue = ConcurrentLinkedQueue<ByteArray>()

    companion object {
        private const val TAG = "VideoDecoder"
        private const val MIME_TYPE = "video/avc" // H.264
        private const val TIMEOUT_US = 10_000L    // 10ms
    }

    fun start() {
        val format = MediaFormat.createVideoFormat(MIME_TYPE, width, height)
        codec = MediaCodec.createDecoderByType(MIME_TYPE).apply {
            configure(format, surface, null, 0)
            start()
        }
        Log.d(TAG, "Decoder started: ${width}x${height}")
    }

    // Called from UDP receiver thread — enqueue NAL unit for decoding
    fun submitNAL(data: ByteArray) {
        nalQueue.offer(data)
    }

    // Run the decode loop on a background thread
    suspend fun decodeLoop() = withContext(Dispatchers.IO) {
        val codec = codec ?: return@withContext
        val bufferInfo = MediaCodec.BufferInfo()

        while (isActive) {
            // Feed input
            val nal = nalQueue.poll()
            if (nal != null) {
                val inputIndex = codec.dequeueInputBuffer(TIMEOUT_US)
                if (inputIndex >= 0) {
                    val inputBuffer = codec.getInputBuffer(inputIndex)!!
                    inputBuffer.clear()
                    inputBuffer.put(nal)
                    codec.queueInputBuffer(inputIndex, 0, nal.size, 0, 0)
                }
            }

            // Drain output
            val outputIndex = codec.dequeueOutputBuffer(bufferInfo, TIMEOUT_US)
            when {
                outputIndex >= 0 -> {
                    codec.releaseOutputBuffer(outputIndex, true) // render = true
                }
                outputIndex == MediaCodec.INFO_OUTPUT_FORMAT_CHANGED -> {
                    Log.d(TAG, "Output format changed: ${codec.outputFormat}")
                }
            }
        }
    }

    fun stop() {
        try {
            codec?.stop()
            codec?.release()
        } catch (e: Exception) {
            Log.w(TAG, "Decoder stop error: ${e.message}")
        }
        codec = null
    }
}
```

**Step 3: Commit**

```bash
git add client/app/src/main/java/com/androidmac/client/video/
git commit -m "feat: add Android UDP receiver and MediaCodec H.264 decoder"
```

---

## Task 14: Android Activities — Wire Everything

**Files:**
- Create: `client/app/src/main/java/com/androidmac/client/MainActivity.kt`
- Create: `client/app/src/main/java/com/androidmac/client/DisplayActivity.kt`

**Step 1: Implement MainActivity (discovery + connect)**

Create `client/app/src/main/java/com/androidmac/client/MainActivity.kt`:

```kotlin
package com.androidmac.client

import android.content.Intent
import android.os.Bundle
import android.widget.Button
import android.widget.TextView
import androidx.appcompat.app.AppCompatActivity
import androidx.lifecycle.lifecycleScope
import com.androidmac.client.control.ControlClient
import com.androidmac.client.discovery.DiscoveredServer
import com.androidmac.client.discovery.NsdDiscovery
import com.google.android.material.textfield.TextInputEditText
import kotlinx.coroutines.launch

class MainActivity : AppCompatActivity() {
    private lateinit var statusText: TextView
    private lateinit var manualIpInput: TextInputEditText
    private lateinit var connectButton: Button
    private lateinit var nsdDiscovery: NsdDiscovery
    private var discoveredServer: DiscoveredServer? = null

    companion object {
        const val EXTRA_HOST = "host"
        const val EXTRA_PORT = "port"
        const val EXTRA_STREAM_PORT = "streamPort"
        const val EXTRA_WIDTH = "width"
        const val EXTRA_HEIGHT = "height"
        const val EXTRA_FPS = "fps"
        private const val DEFAULT_CONTROL_PORT = 9000
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContentView(R.layout.activity_main)

        statusText = findViewById(R.id.statusText)
        manualIpInput = findViewById(R.id.manualIpInput)
        connectButton = findViewById(R.id.connectButton)

        nsdDiscovery = NsdDiscovery(this)

        // Start scanning
        lifecycleScope.launch {
            nsdDiscovery.discover().collect { server ->
                discoveredServer = server
                statusText.text = "Found: ${server.name} (${server.host}:${server.port})"
            }
        }

        connectButton.setOnClickListener {
            val manualIp = manualIpInput.text?.toString()?.trim()
            val host: String
            val port: Int

            if (!manualIp.isNullOrEmpty()) {
                host = manualIp
                port = DEFAULT_CONTROL_PORT
            } else if (discoveredServer != null) {
                host = discoveredServer!!.host
                port = discoveredServer!!.port
            } else {
                statusText.text = "No server found. Enter IP manually."
                return@setOnClickListener
            }

            connectToServer(host, port)
        }
    }

    private fun connectToServer(host: String, port: Int) {
        statusText.text = "Connecting to $host:$port..."
        connectButton.isEnabled = false

        lifecycleScope.launch {
            try {
                val client = ControlClient()
                val metrics = resources.displayMetrics
                val serverHello = client.connect(host, port, metrics)

                // Launch display activity
                val intent = Intent(this@MainActivity, DisplayActivity::class.java).apply {
                    putExtra(EXTRA_HOST, host)
                    putExtra(EXTRA_PORT, port)
                    putExtra(EXTRA_STREAM_PORT, serverHello.streamPort)
                    putExtra(EXTRA_WIDTH, serverHello.virtualDisplay.width)
                    putExtra(EXTRA_HEIGHT, serverHello.virtualDisplay.height)
                    putExtra(EXTRA_FPS, serverHello.fps)
                }
                startActivity(intent)
            } catch (e: Exception) {
                statusText.text = "Connection failed: ${e.message}"
                connectButton.isEnabled = true
            }
        }
    }
}
```

**Step 2: Implement DisplayActivity (fullscreen + decode)**

Create `client/app/src/main/java/com/androidmac/client/DisplayActivity.kt`:

```kotlin
package com.androidmac.client

import android.os.Bundle
import android.view.SurfaceHolder
import android.view.SurfaceView
import android.view.WindowInsets
import android.view.WindowInsetsController
import androidx.appcompat.app.AppCompatActivity
import androidx.lifecycle.lifecycleScope
import com.androidmac.client.video.UdpReceiver
import com.androidmac.client.video.VideoDecoder
import kotlinx.coroutines.Job
import kotlinx.coroutines.launch
import java.util.concurrent.ConcurrentHashMap

class DisplayActivity : AppCompatActivity() {
    private lateinit var surfaceView: SurfaceView
    private var decoder: VideoDecoder? = null
    private var receiver: UdpReceiver? = null
    private var receiveJob: Job? = null
    private var decodeJob: Job? = null

    // Fragment reassembly buffer: sequence -> fragments
    private val fragmentBuffer = ConcurrentHashMap<Long, Array<ByteArray?>>()

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContentView(R.layout.activity_display)

        // Immersive fullscreen
        window.insetsController?.let {
            it.hide(WindowInsets.Type.systemBars())
            it.systemBarsBehavior = WindowInsetsController.BEHAVIOR_SHOW_TRANSIENT_BARS_BY_SWIPE
        }

        val host = intent.getStringExtra(MainActivity.EXTRA_HOST) ?: return
        val streamPort = intent.getIntExtra(MainActivity.EXTRA_STREAM_PORT, 9001)
        val width = intent.getIntExtra(MainActivity.EXTRA_WIDTH, 2000)
        val height = intent.getIntExtra(MainActivity.EXTRA_HEIGHT, 1200)

        surfaceView = findViewById(R.id.surfaceView)
        surfaceView.holder.addCallback(object : SurfaceHolder.Callback {
            override fun surfaceCreated(holder: SurfaceHolder) {
                startStreaming(holder, width, height, streamPort)
            }

            override fun surfaceChanged(holder: SurfaceHolder, format: Int, w: Int, h: Int) {}

            override fun surfaceDestroyed(holder: SurfaceHolder) {
                stopStreaming()
            }
        })
    }

    private fun startStreaming(holder: SurfaceHolder, width: Int, height: Int, port: Int) {
        val dec = VideoDecoder(width, height, holder.surface)
        dec.start()
        decoder = dec

        val recv = UdpReceiver(port)
        receiver = recv

        // Decode loop
        decodeJob = lifecycleScope.launch {
            dec.decodeLoop()
        }

        // Receive loop
        receiveJob = lifecycleScope.launch {
            recv.receiveLoop { packet ->
                if (packet.fragTotal == 1) {
                    // Single packet — submit directly
                    dec.submitNAL(packet.payload)
                } else {
                    // Multi-fragment — reassemble
                    val frags = fragmentBuffer.getOrPut(packet.sequence) {
                        arrayOfNulls(packet.fragTotal)
                    }
                    frags[packet.fragIndex] = packet.payload

                    // Check if all fragments received
                    if (frags.all { it != null }) {
                        fragmentBuffer.remove(packet.sequence)
                        val combined = frags.fold(ByteArray(0)) { acc, frag ->
                            acc + frag!!
                        }
                        dec.submitNAL(combined)
                    }
                }
            }
        }
    }

    private fun stopStreaming() {
        receiveJob?.cancel()
        decodeJob?.cancel()
        receiver?.close()
        decoder?.stop()
    }

    override fun onDestroy() {
        super.onDestroy()
        stopStreaming()
    }
}
```

**Step 3: Commit**

```bash
git add client/app/src/main/java/com/androidmac/client/MainActivity.kt
git add client/app/src/main/java/com/androidmac/client/DisplayActivity.kt
git commit -m "feat: add Android activities with discovery, handshake, and video display"
```

---

## Task 15: End-to-End Integration Test

**Step 1: Build and run Mac server**

```bash
cd /Users/luke/sideProject/android-mac/server
go build -o android-mac-server ./cmd/server/
./android-mac-server
```

Expected output:
```
android-mac server starting...
mDNS advertising on port 9000
control server listening on port 9000
```

**Step 2: Build and install Android app**

Open `client/` in Android Studio, connect Tab S6 Lite via USB, build and install.

**Step 3: Verify discovery**

On the Android app, you should see the Mac appear in the device list (or enter IP manually).

**Step 4: Connect and verify video**

1. Tap Connect on Android
2. Mac server should log: `client connected: SM-P613 (2000x1200)`
3. Mac should create virtual display (check System Settings > Displays)
4. Android should show the Mac extended desktop

**Step 5: Verify by dragging a window**

On Mac, drag a window to the virtual display area. It should appear on the tablet.

**Step 6: Document any issues found**

Create `docs/plans/phase1-issues.md` with any issues encountered during testing.

**Step 7: Final commit**

```bash
git add -A
git commit -m "feat: Phase 1 complete — Mac→Android screen streaming over WiFi"
```

---

## Summary

| Task | Component | Type | Testable |
|------|-----------|------|----------|
| 1 | Go project setup | Scaffold | Build check |
| 2 | Packet format | Pure Go | Unit tests |
| 3 | mDNS discovery | Go + zeroconf | Integration test |
| 4 | TCP control server | Pure Go | Unit tests |
| 5 | UDP streamer | Pure Go | Unit tests |
| 6 | Virtual display | CGo + ObjC | Manual (Mac only) |
| 7 | Screen capture | CGo + ObjC | Manual (Mac only) |
| 8 | H.264 encoder | CGo + ObjC | Manual (Mac only) |
| 9 | Mac server main | Go | Build + manual |
| 10 | Android project | Scaffold | Build check |
| 11 | Android NSD | Kotlin | Manual (device) |
| 12 | Android control | Kotlin | Manual (device) |
| 13 | Android video | Kotlin | Manual (device) |
| 14 | Android activities | Kotlin | Manual (device) |
| 15 | Integration test | End-to-end | Manual |
