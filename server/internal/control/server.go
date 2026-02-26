package control

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"sync"

	"github.com/luke/android-mac/server/internal/protocol"
)

type ClientConn struct {
	Conn  net.Conn
	Hello protocol.ClientHello
}

type Server struct {
	listener   net.Listener
	clients    []ClientConn
	mu         sync.Mutex
	streamPort int
	onClient   func(ClientConn)
	done       chan struct{}
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
	var hello protocol.ClientHello
	decoder := json.NewDecoder(conn)
	if err := decoder.Decode(&hello); err != nil {
		log.Printf("handshake read error: %v", err)
		conn.Close()
		return
	}

	log.Printf("client connected: %s (%dx%d)", hello.Device, hello.Screen.Width, hello.Screen.Height)

	codec := "h264"
	for _, c := range hello.Codecs {
		if c == "h264" {
			codec = "h264"
			break
		}
	}

	response := protocol.ServerHello{
		VirtualDisplay: protocol.DisplayInfo{
			Width:  hello.Screen.Width,
			Height: hello.Screen.Height,
		},
		Codec:      codec,
		Bitrate:    8_000_000,
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

func (s *Server) Stop() {
	close(s.done)
	s.listener.Close()
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, c := range s.clients {
		c.Conn.Close()
	}
}
