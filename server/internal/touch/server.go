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
	s.handler = handler
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
			if err != io.EOF {
				log.Printf("touch read error: %v", err)
			}
			return
		}
		if s.handler != nil {
			event := Unmarshal(buf)
			s.handler(event)
		}
	}
}

func (s *Server) Stop() {
	close(s.done)
	s.listener.Close()
	s.wg.Wait()
}
