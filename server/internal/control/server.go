package control

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"sync"
	"time"

	"github.com/luke/android-mac/server/internal/protocol"
)

type ClientConn struct {
	Conn      net.Conn
	Hello     protocol.ClientHello
	UDPPort   int // actual UDP port reported by the client via ClientReady (0 for USB)
	TouchPort int // TCP touch port allocated for this client
	VideoPort int // TCP video port allocated for this client (0 for WiFi/UDP mode)
}

// PortAllocator allocates a per-client touch (and, for USB clients, video) port
// before the ServerHello is sent. Called once per incoming connection.
type PortAllocator func(conn net.Conn, hello protocol.ClientHello) (touchPort, videoPort int, err error)

type Server struct {
	listener      net.Listener
	clients       []ClientConn
	mu            sync.Mutex
	streamPort    int
	allocatePorts PortAllocator
	onClient      func(ClientConn)
	done          chan struct{}
}

func NewServer(port int) (*Server, error) {
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return nil, err
	}
	return &Server{
		listener:   ln,
		streamPort: 5001,
		done:       make(chan struct{}),
	}, nil
}

func (s *Server) Addr() string {
	return s.listener.Addr().String()
}

func (s *Server) Port() int {
	return s.listener.Addr().(*net.TCPAddr).Port
}

func (s *Server) SetStreamPort(port int) {
	s.streamPort = port
}

// SetPortAllocator registers the callback used to allocate a per-client
// touch/video port pair during the handshake, before ServerHello is sent.
func (s *Server) SetPortAllocator(fn PortAllocator) {
	s.allocatePorts = fn
}

func (s *Server) OnClient(fn func(ClientConn)) {
	s.onClient = fn
}

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
	// I6: Enforce a 10-second deadline for the entire handshake phase.
	conn.SetReadDeadline(time.Now().Add(10 * time.Second))

	decoder := json.NewDecoder(conn)

	// Step 1: Read ClientHello
	var hello protocol.ClientHello
	if err := decoder.Decode(&hello); err != nil {
		log.Printf("handshake read error: %v", err)
		conn.Close()
		return
	}

	connType := "WiFi"
	if hello.IsUSB() {
		connType = "USB"
	}
	log.Printf("client connected: %s (%dx%d) [%s]", hello.Device, hello.Screen.Width, hello.Screen.Height, connType)

	codec := "h264"
	for _, c := range hello.Codecs {
		if c == "h264" {
			codec = "h264"
			break
		}
	}

	// Step 2: Allocate this client's touch/video ports, then send ServerHello.
	var touchPort, videoPort int
	if s.allocatePorts != nil {
		var err error
		touchPort, videoPort, err = s.allocatePorts(conn, hello)
		if err != nil {
			log.Printf("port allocation error: %v", err)
			conn.Close()
			return
		}
	}

	bitrate := hello.Bitrate
	if bitrate <= 0 {
		bitrate = 8_000_000 // default
	}
	response := protocol.ServerHello{
		VirtualDisplay: protocol.DisplayInfo{
			Width:  hello.Screen.Width,
			Height: hello.Screen.Height,
		},
		Codec:      codec,
		Bitrate:    bitrate,
		FPS:        60,
		StreamPort: s.streamPort,
		TouchPort:  touchPort,
		VideoPort:  videoPort,
	}

	enc := json.NewEncoder(conn)
	if err := enc.Encode(response); err != nil {
		log.Printf("handshake write error: %v", err)
		conn.Close()
		return
	}

	// Step 3: Read ClientReady to get the actual UDP port
	var ready protocol.ClientReady
	if err := decoder.Decode(&ready); err != nil {
		log.Printf("client ready read error: %v", err)
		conn.Close()
		return
	}
	if hello.IsUSB() {
		log.Println("client ready: USB mode (TCP video)")
	} else {
		log.Printf("client ready: UDP port %d", ready.UDPPort)
	}

	// Clear the read deadline after successful handshake.
	conn.SetReadDeadline(time.Time{})

	client := ClientConn{Conn: conn, Hello: hello, UDPPort: ready.UDPPort, TouchPort: touchPort, VideoPort: videoPort}
	s.mu.Lock()
	s.clients = append(s.clients, client)
	s.mu.Unlock()

	if s.onClient != nil {
		s.onClient(client)
	}
}

func (s *Server) Stop() {
	close(s.done)
	s.listener.Close()
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, c := range s.clients {
		c.Conn.Close()
	}
}
