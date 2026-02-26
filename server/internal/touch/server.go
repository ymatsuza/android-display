package touch

import (
	"fmt"
	"io"
	"log"
	"net"
	"sync"
)

type EventHandler func(Event)

type Server struct {
	listener net.Listener
	mu       sync.Mutex // protects handler
	handler  EventHandler
	done     chan struct{}
	wg       sync.WaitGroup
}

func NewServer(port int) (*Server, error) {
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return nil, err
	}
	return &Server{
		listener: ln,
		done:     make(chan struct{}),
	}, nil
}

func (s *Server) Port() int {
	return s.listener.Addr().(*net.TCPAddr).Port
}

func (s *Server) OnEvent(handler EventHandler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handler = handler
}

func (s *Server) getHandler() EventHandler {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.handler
}

func (s *Server) AcceptLoop() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			select {
			case <-s.done:
				return
			default:
				log.Printf("touch accept error: %v", err)
				continue
			}
		}
		s.wg.Add(1)
		go s.handleConn(conn)
	}
}

func (s *Server) handleConn(conn net.Conn) {
	defer s.wg.Done()
	defer conn.Close()

	buf := make([]byte, EventSize)
	for {
		_, err := io.ReadFull(conn, buf)
		if err != nil {
			if err != io.EOF && err != io.ErrUnexpectedEOF {
				log.Printf("touch read error: %v", err)
			}
			return
		}
		if h := s.getHandler(); h != nil {
			event := Unmarshal(buf)
			h(event)
		}
	}
}

func (s *Server) Stop() {
	close(s.done)
	s.listener.Close()
	s.wg.Wait()
}
