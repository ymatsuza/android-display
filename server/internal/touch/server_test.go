package touch

import (
	"fmt"
	"net"
	"testing"
	"time"
)

func TestServerReceivesEvent(t *testing.T) {
	srv, err := NewServer(0)
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Stop()

	received := make(chan Event, 10)
	srv.OnEvent(func(e Event) {
		received <- e
	})
	go srv.AcceptLoop()

	conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", srv.Port()))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	event := Event{
		Type: TouchTypeFinger, Action: TouchActionDown,
		X: 0.5, Y: 0.3, Pressure: 0.0, PointerID: 0, Timestamp: 100,
	}
	_, err = conn.Write(event.Marshal())
	if err != nil {
		t.Fatal(err)
	}

	select {
	case got := <-received:
		if got.Type != event.Type {
			t.Fatalf("type mismatch: got %d want %d", got.Type, event.Type)
		}
		if got.Action != event.Action {
			t.Fatalf("action mismatch: got %d want %d", got.Action, event.Action)
		}
		if got.X != event.X || got.Y != event.Y {
			t.Fatalf("coord mismatch: got (%f,%f) want (%f,%f)", got.X, got.Y, event.X, event.Y)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for event")
	}
}

func TestServerMultipleEvents(t *testing.T) {
	srv, err := NewServer(0)
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Stop()

	received := make(chan Event, 100)
	srv.OnEvent(func(e Event) {
		received <- e
	})
	go srv.AcceptLoop()

	conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", srv.Port()))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	events := []Event{
		{Type: TouchTypeFinger, Action: TouchActionDown, X: 0.5, Y: 0.5, PointerID: 0, Timestamp: 100},
		{Type: TouchTypeFinger, Action: TouchActionMove, X: 0.6, Y: 0.5, PointerID: 0, Timestamp: 110},
		{Type: TouchTypeFinger, Action: TouchActionUp, X: 0.6, Y: 0.5, PointerID: 0, Timestamp: 120},
	}

	for _, e := range events {
		if _, err := conn.Write(e.Marshal()); err != nil {
			t.Fatal(err)
		}
	}

	for i, want := range events {
		select {
		case got := <-received:
			if got.Action != want.Action {
				t.Fatalf("event %d: action mismatch: got %d want %d", i, got.Action, want.Action)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("timeout waiting for event %d", i)
		}
	}
}

func TestServerPort(t *testing.T) {
	srv, err := NewServer(0)
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Stop()

	port := srv.Port()
	if port <= 0 {
		t.Fatalf("expected positive port, got %d", port)
	}
}
