# Phase 2: Touch Support Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Enable finger touch on Android tablet to control macOS via the extended display — tap→click, drag, two-finger scroll, long-press→right-click, double-tap.

**Architecture:** Android captures raw touch events (finger coordinates, pointer IDs) and sends them as fixed-size 26-byte binary packets over a dedicated TCP connection. Mac server receives these events, runs them through a gesture recognizer state machine, and injects the resulting mouse actions via CGEvent API. The touch port is communicated via the existing handshake protocol (new `touchPort` field in ServerHello).

**Tech Stack:** Go (gesture recognizer, CGo for CGEvent), Kotlin (Android touch capture + TCP sender), Binary protocol over TCP

**Note:** The design doc specifies protobuf for touch serialization, but we use a simpler fixed-size binary format instead. This achieves the same goals (compact at 26 bytes, fast) without requiring protobuf toolchain setup. At 120Hz, this is ~3KB/s — trivial on WiFi.

**Important:** CGEvent injection requires macOS Accessibility permission. On first run, the user must grant access via System Preferences → Privacy & Security → Accessibility.

---

### Task 1: Touch Event Binary Protocol (Go)

**Files:**
- Create: `server/internal/touch/event.go`
- Create: `server/internal/touch/event_test.go`

**Context:** This defines the shared binary protocol for touch events sent from Android→Mac. Fixed-size 26-byte format matching the video packet pattern already in the codebase.

**Step 1: Write the failing test**

Create `server/internal/touch/event_test.go`:

```go
package touch

import (
	"math"
	"testing"
)

func TestEventMarshalRoundTrip(t *testing.T) {
	event := Event{
		Type:      TouchTypeFinger,
		Action:    TouchActionDown,
		X:         0.5,
		Y:         0.75,
		Pressure:  0.0,
		PointerID: 1,
		Timestamp: 1234567890,
	}

	buf := event.Marshal()
	if len(buf) != EventSize {
		t.Fatalf("expected %d bytes, got %d", EventSize, len(buf))
	}

	decoded := Unmarshal(buf)
	if decoded != event {
		t.Fatalf("round trip failed: got %+v, want %+v", decoded, event)
	}
}

func TestEventPenWithPressure(t *testing.T) {
	event := Event{
		Type:      TouchTypePen,
		Action:    TouchActionMove,
		X:         0.123,
		Y:         0.456,
		Pressure:  0.789,
		PointerID: 0,
		Timestamp: 9876543210,
	}

	buf := event.Marshal()
	decoded := Unmarshal(buf)

	if decoded.Type != TouchTypePen {
		t.Fatalf("expected PEN type, got %d", decoded.Type)
	}
	if decoded.Action != TouchActionMove {
		t.Fatalf("expected MOVE action, got %d", decoded.Action)
	}
	if math.Abs(float64(decoded.Pressure-0.789)) > 0.001 {
		t.Fatalf("pressure mismatch: got %f, want 0.789", decoded.Pressure)
	}
	if decoded.Timestamp != 9876543210 {
		t.Fatalf("timestamp mismatch: got %d", decoded.Timestamp)
	}
}

func TestEventSize(t *testing.T) {
	// Verify constant matches actual serialization
	e := Event{}
	buf := e.Marshal()
	if len(buf) != 26 {
		t.Fatalf("event should be 26 bytes, got %d", len(buf))
	}
}

func TestEventBoundaryValues(t *testing.T) {
	// Test normalized coordinates at boundaries
	event := Event{
		Type:      TouchTypeFinger,
		Action:    TouchActionUp,
		X:         0.0,
		Y:         1.0,
		Pressure:  1.0,
		PointerID: -1,
		Timestamp: 0,
	}

	buf := event.Marshal()
	decoded := Unmarshal(buf)
	if decoded.X != 0.0 || decoded.Y != 1.0 {
		t.Fatalf("boundary values not preserved: x=%f y=%f", decoded.X, decoded.Y)
	}
	if decoded.PointerID != -1 {
		t.Fatalf("negative pointer ID not preserved: %d", decoded.PointerID)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `cd /Users/luke/sideProject/android-mac/server && go test ./internal/touch/ -v`
Expected: FAIL — package/types not found

**Step 3: Write the implementation**

Create `server/internal/touch/event.go`:

```go
package touch

import (
	"encoding/binary"
	"math"
)

// TouchType distinguishes finger from pen/stylus input.
type TouchType byte

const (
	TouchTypeFinger TouchType = 0
	TouchTypePen    TouchType = 1
)

// TouchAction represents the phase of a touch event.
type TouchAction byte

const (
	TouchActionDown TouchAction = 0
	TouchActionMove TouchAction = 1
	TouchActionUp   TouchAction = 2
)

// EventSize is the fixed byte size of a serialized touch event.
//
// Layout (26 bytes, big-endian):
//   Byte  0:     Type      (uint8)
//   Byte  1:     Action    (uint8)
//   Bytes 2-5:   X         (float32)
//   Bytes 6-9:   Y         (float32)
//   Bytes 10-13: Pressure  (float32)
//   Bytes 14-17: PointerID (int32)
//   Bytes 18-25: Timestamp (int64, milliseconds)
const EventSize = 26

// Event represents a single touch/pen event from the Android client.
// Coordinates X and Y are normalized to 0.0–1.0 range.
type Event struct {
	Type      TouchType
	Action    TouchAction
	X         float32
	Y         float32
	Pressure  float32
	PointerID int32
	Timestamp int64
}

// Marshal serializes the event into a fixed-size byte slice.
func (e *Event) Marshal() []byte {
	buf := make([]byte, EventSize)
	buf[0] = byte(e.Type)
	buf[1] = byte(e.Action)
	binary.BigEndian.PutUint32(buf[2:6], math.Float32bits(e.X))
	binary.BigEndian.PutUint32(buf[6:10], math.Float32bits(e.Y))
	binary.BigEndian.PutUint32(buf[10:14], math.Float32bits(e.Pressure))
	binary.BigEndian.PutUint32(buf[14:18], uint32(e.PointerID))
	binary.BigEndian.PutUint64(buf[18:26], uint64(e.Timestamp))
	return buf
}

// Unmarshal deserializes a touch event from a byte slice.
// The slice must be at least EventSize bytes.
func Unmarshal(buf []byte) Event {
	return Event{
		Type:      TouchType(buf[0]),
		Action:    TouchAction(buf[1]),
		X:         math.Float32frombits(binary.BigEndian.Uint32(buf[2:6])),
		Y:         math.Float32frombits(binary.BigEndian.Uint32(buf[6:10])),
		Pressure:  math.Float32frombits(binary.BigEndian.Uint32(buf[10:14])),
		PointerID: int32(binary.BigEndian.Uint32(buf[14:18])),
		Timestamp: int64(binary.BigEndian.Uint64(buf[18:26])),
	}
}
```

**Step 4: Run tests to verify they pass**

Run: `cd /Users/luke/sideProject/android-mac/server && go test ./internal/touch/ -v`
Expected: PASS — all 4 tests

**Step 5: Commit**

```bash
cd /Users/luke/sideProject/android-mac
git add server/internal/touch/event.go server/internal/touch/event_test.go
git commit -m "功能：新增觸控事件二進位協定（26 字節固定格式）"
```

---

### Task 2: Touch TCP Server (Go)

**Files:**
- Create: `server/internal/touch/server.go`
- Create: `server/internal/touch/server_test.go`

**Context:** TCP server that listens for touch connections from Android and reads the fixed-size 26-byte binary events. Uses the same pattern as `internal/control/server.go` but reads binary instead of JSON. Listens on port 0 (auto-assign) so the port can be reported in ServerHello.

**Step 1: Write the failing test**

Create `server/internal/touch/server_test.go`:

```go
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

	// Connect as client
	conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", srv.Port()))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	// Send a touch event
	event := Event{
		Type:      TouchTypeFinger,
		Action:    TouchActionDown,
		X:         0.5,
		Y:         0.3,
		Pressure:  0.0,
		PointerID: 0,
		Timestamp: 100,
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

	// Send down + move + up sequence
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
```

**Step 2: Run test to verify it fails**

Run: `cd /Users/luke/sideProject/android-mac/server && go test ./internal/touch/ -v -run TestServer`
Expected: FAIL — NewServer not found

**Step 3: Write the implementation**

Create `server/internal/touch/server.go`:

```go
package touch

import (
	"io"
	"log"
	"net"
	"fmt"
	"sync"
)

// EventHandler is called for each received touch event.
type EventHandler func(Event)

// Server listens for touch TCP connections from Android and delivers
// fixed-size binary touch events to a registered handler.
type Server struct {
	listener net.Listener
	handler  EventHandler
	done     chan struct{}
	wg       sync.WaitGroup
}

// NewServer creates a touch TCP server on the given port.
// Use port 0 for auto-assign.
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

// Port returns the listening port.
func (s *Server) Port() int {
	return s.listener.Addr().(*net.TCPAddr).Port
}

// OnEvent registers the callback for received touch events.
func (s *Server) OnEvent(handler EventHandler) {
	s.handler = handler
}

// AcceptLoop accepts TCP connections and reads touch events from each.
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

// Stop shuts down the server and waits for connections to close.
func (s *Server) Stop() {
	close(s.done)
	s.listener.Close()
	s.wg.Wait()
}
```

**Step 4: Run tests to verify they pass**

Run: `cd /Users/luke/sideProject/android-mac/server && go test ./internal/touch/ -v`
Expected: PASS — all 7 tests (4 event + 3 server)

**Step 5: Commit**

```bash
cd /Users/luke/sideProject/android-mac
git add server/internal/touch/server.go server/internal/touch/server_test.go
git commit -m "功能：新增觸控 TCP 伺服器（二進位事件接收）"
```

---

### Task 3: CGEvent Input Injector (Go + CGo)

**Files:**
- Create: `server/bridge/input_bridge.h`
- Create: `server/internal/input/input_bridge.m`
- Create: `server/internal/input/injector.go`

**Context:** CGo bridge for injecting mouse events (move, click, drag, right-click, scroll) via macOS CGEvent API. Follows the same CGo pattern as display/capture/encoder: C header in `bridge/`, ObjC implementation in the Go package dir, Go wrapper with `#cgo` directives. Converts normalized 0.0–1.0 touch coordinates to global screen coordinates using `CGDisplayBounds`.

**Important:** CGEvent injection requires Accessibility permission. The injector should check `AXIsProcessTrusted()` and log a warning if not granted.

**Step 1: Create C header**

Create `server/bridge/input_bridge.h`:

```c
#ifndef INPUT_BRIDGE_H
#define INPUT_BRIDGE_H

#include <stdint.h>
#include <stdbool.h>

typedef struct {
    double x;
    double y;
    double width;
    double height;
} DisplayBounds;

// Returns the display bounds in the global coordinate space.
DisplayBounds GetDisplayBounds(uint32_t displayID);

// Returns true if the process has Accessibility permission.
bool CheckAccessibilityPermission(void);

// Mouse event injection functions.
// All coordinates are in the global display coordinate space (points).
void InjectMouseMove(double x, double y);
void InjectLeftMouseDown(double x, double y);
void InjectLeftMouseUp(double x, double y);
void InjectLeftMouseDragged(double x, double y);
void InjectRightMouseDown(double x, double y);
void InjectRightMouseUp(double x, double y);
void InjectScrollWheel(int32_t deltaX, int32_t deltaY);

#endif
```

**Step 2: Create ObjC implementation**

Create `server/internal/input/input_bridge.m`:

```objc
#import <CoreGraphics/CoreGraphics.h>
#import <ApplicationServices/ApplicationServices.h>
#include "../../bridge/input_bridge.h"

DisplayBounds GetDisplayBounds(uint32_t displayID) {
    CGRect rect = CGDisplayBounds(displayID);
    DisplayBounds bounds;
    bounds.x = rect.origin.x;
    bounds.y = rect.origin.y;
    bounds.width = rect.size.width;
    bounds.height = rect.size.height;
    return bounds;
}

bool CheckAccessibilityPermission(void) {
    return AXIsProcessTrusted();
}

void InjectMouseMove(double x, double y) {
    CGEventRef event = CGEventCreateMouseEvent(NULL, kCGEventMouseMoved, CGPointMake(x, y), kCGMouseButtonLeft);
    if (event) {
        CGEventPost(kCGHIDEventTap, event);
        CFRelease(event);
    }
}

void InjectLeftMouseDown(double x, double y) {
    CGEventRef event = CGEventCreateMouseEvent(NULL, kCGEventLeftMouseDown, CGPointMake(x, y), kCGMouseButtonLeft);
    if (event) {
        CGEventPost(kCGHIDEventTap, event);
        CFRelease(event);
    }
}

void InjectLeftMouseUp(double x, double y) {
    CGEventRef event = CGEventCreateMouseEvent(NULL, kCGEventLeftMouseUp, CGPointMake(x, y), kCGMouseButtonLeft);
    if (event) {
        CGEventPost(kCGHIDEventTap, event);
        CFRelease(event);
    }
}

void InjectLeftMouseDragged(double x, double y) {
    CGEventRef event = CGEventCreateMouseEvent(NULL, kCGEventLeftMouseDragged, CGPointMake(x, y), kCGMouseButtonLeft);
    if (event) {
        CGEventPost(kCGHIDEventTap, event);
        CFRelease(event);
    }
}

void InjectRightMouseDown(double x, double y) {
    CGEventRef event = CGEventCreateMouseEvent(NULL, kCGEventRightMouseDown, CGPointMake(x, y), kCGMouseButtonRight);
    if (event) {
        CGEventPost(kCGHIDEventTap, event);
        CFRelease(event);
    }
}

void InjectRightMouseUp(double x, double y) {
    CGEventRef event = CGEventCreateMouseEvent(NULL, kCGEventRightMouseUp, CGPointMake(x, y), kCGMouseButtonRight);
    if (event) {
        CGEventPost(kCGHIDEventTap, event);
        CFRelease(event);
    }
}

void InjectScrollWheel(int32_t deltaX, int32_t deltaY) {
    CGEventRef event = CGEventCreateScrollWheelEvent(NULL, kCGScrollEventUnitPixel, 2, deltaY, deltaX);
    if (event) {
        CGEventPost(kCGHIDEventTap, event);
        CFRelease(event);
    }
}
```

**Step 3: Create Go wrapper**

Create `server/internal/input/injector.go`:

```go
package input

/*
#cgo CFLAGS: -I../../bridge
#cgo LDFLAGS: -framework CoreGraphics -framework ApplicationServices
#include "input_bridge.h"
*/
import "C"

import "log"

// DisplayBounds represents a display's position and size in global coordinates.
type DisplayBounds struct {
	X, Y, Width, Height float64
}

// Injector injects mouse events targeted at a specific virtual display.
type Injector struct {
	displayID uint32
	bounds    DisplayBounds
}

// NewInjector creates an injector for the given display.
// It queries the display bounds and checks for Accessibility permission.
func NewInjector(displayID uint32) *Injector {
	if !bool(C.CheckAccessibilityPermission()) {
		log.Println("WARNING: Accessibility permission not granted!")
		log.Println("Go to System Preferences → Privacy & Security → Accessibility")
		log.Println("and add this application to enable touch input.")
	}

	cb := C.GetDisplayBounds(C.uint32_t(displayID))
	inj := &Injector{
		displayID: displayID,
		bounds: DisplayBounds{
			X:      float64(cb.x),
			Y:      float64(cb.y),
			Width:  float64(cb.width),
			Height: float64(cb.height),
		},
	}
	log.Printf("injector: display 0x%x bounds: (%.0f,%.0f) %.0fx%.0f",
		displayID, inj.bounds.X, inj.bounds.Y, inj.bounds.Width, inj.bounds.Height)
	return inj
}

// ToScreenCoords converts normalized touch coordinates (0.0–1.0) to global screen points.
func (inj *Injector) ToScreenCoords(nx, ny float32) (float64, float64) {
	x := inj.bounds.X + float64(nx)*inj.bounds.Width
	y := inj.bounds.Y + float64(ny)*inj.bounds.Height
	return x, y
}

// Bounds returns the display bounds.
func (inj *Injector) Bounds() DisplayBounds {
	return inj.bounds
}

// MouseMove moves the cursor without clicking.
func (inj *Injector) MouseMove(nx, ny float32) {
	x, y := inj.ToScreenCoords(nx, ny)
	C.InjectMouseMove(C.double(x), C.double(y))
}

// LeftMouseDown presses the left mouse button.
func (inj *Injector) LeftMouseDown(nx, ny float32) {
	x, y := inj.ToScreenCoords(nx, ny)
	C.InjectLeftMouseDown(C.double(x), C.double(y))
}

// LeftMouseUp releases the left mouse button.
func (inj *Injector) LeftMouseUp(nx, ny float32) {
	x, y := inj.ToScreenCoords(nx, ny)
	C.InjectLeftMouseUp(C.double(x), C.double(y))
}

// LeftMouseDragged moves the cursor while the left button is held.
func (inj *Injector) LeftMouseDragged(nx, ny float32) {
	x, y := inj.ToScreenCoords(nx, ny)
	C.InjectLeftMouseDragged(C.double(x), C.double(y))
}

// RightMouseDown presses the right mouse button (context menu).
func (inj *Injector) RightMouseDown(nx, ny float32) {
	x, y := inj.ToScreenCoords(nx, ny)
	C.InjectRightMouseDown(C.double(x), C.double(y))
}

// RightMouseUp releases the right mouse button.
func (inj *Injector) RightMouseUp(nx, ny float32) {
	x, y := inj.ToScreenCoords(nx, ny)
	C.InjectRightMouseUp(C.double(x), C.double(y))
}

// ScrollWheel injects a scroll wheel event with pixel deltas.
func (inj *Injector) ScrollWheel(deltaX, deltaY int32) {
	C.InjectScrollWheel(C.int32_t(deltaX), C.int32_t(deltaY))
}
```

**Step 4: Verify it compiles**

Run: `cd /Users/luke/sideProject/android-mac/server && go build ./internal/input/`
Expected: builds without errors (no unit tests for CGo — it requires real display hardware)

**Step 5: Commit**

```bash
cd /Users/luke/sideProject/android-mac
git add server/bridge/input_bridge.h server/internal/input/input_bridge.m server/internal/input/injector.go
git commit -m "功能：新增 CGEvent 滑鼠事件注入橋接（移動、點擊、拖曳、捲動）"
```

---

### Task 4: Gesture Recognizer (Go)

**Files:**
- Create: `server/internal/input/gesture.go`
- Create: `server/internal/input/gesture_test.go`

**Context:** Pure Go state machine that converts raw touch events into mouse actions. No CGo dependency — it takes touch events and calls the Injector interface. Supports: tap→click, drag, two-finger scroll, long-press→right-click, double-tap→double-click.

The gesture recognizer is tested with a mock handler, no real display needed.

**Step 1: Write the failing tests**

Create `server/internal/input/gesture_test.go`:

```go
package input

import (
	"sync"
	"testing"
	"time"

	"github.com/luke/android-mac/server/internal/touch"
)

// mockRecorder collects all mouse events emitted by the gesture recognizer.
type mockRecorder struct {
	mu     sync.Mutex
	events []MouseEvent
}

func (m *mockRecorder) handler(e MouseEvent) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = append(m.events, e)
}

func (m *mockRecorder) get() []MouseEvent {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]MouseEvent, len(m.events))
	copy(cp, m.events)
	return cp
}

func (m *mockRecorder) hasAction(action MouseActionType) bool {
	for _, e := range m.get() {
		if e.Action == action {
			return true
		}
	}
	return false
}

func (m *mockRecorder) countAction(action MouseActionType) int {
	count := 0
	for _, e := range m.get() {
		if e.Action == action {
			count++
		}
	}
	return count
}

func TestTapGeneratesClick(t *testing.T) {
	rec := &mockRecorder{}
	gr := NewGestureRecognizer(rec.handler)

	gr.HandleEvent(touch.Event{
		Type: touch.TouchTypeFinger, Action: touch.TouchActionDown,
		X: 0.5, Y: 0.5, PointerID: 0, Timestamp: 100,
	})
	gr.HandleEvent(touch.Event{
		Type: touch.TouchTypeFinger, Action: touch.TouchActionUp,
		X: 0.5, Y: 0.5, PointerID: 0, Timestamp: 150,
	})

	// Wait for double-tap timeout to fire single click
	time.Sleep(400 * time.Millisecond)

	if !rec.hasAction(ActionLeftDown) {
		t.Fatal("expected LeftDown from tap")
	}
	if !rec.hasAction(ActionLeftUp) {
		t.Fatal("expected LeftUp from tap")
	}
}

func TestDragGeneratesMouseDrag(t *testing.T) {
	rec := &mockRecorder{}
	gr := NewGestureRecognizer(rec.handler)

	// Down
	gr.HandleEvent(touch.Event{
		Type: touch.TouchTypeFinger, Action: touch.TouchActionDown,
		X: 0.5, Y: 0.5, PointerID: 0, Timestamp: 100,
	})
	// Move far enough to exceed drag threshold
	gr.HandleEvent(touch.Event{
		Type: touch.TouchTypeFinger, Action: touch.TouchActionMove,
		X: 0.6, Y: 0.6, PointerID: 0, Timestamp: 120,
	})
	// Move more
	gr.HandleEvent(touch.Event{
		Type: touch.TouchTypeFinger, Action: touch.TouchActionMove,
		X: 0.7, Y: 0.7, PointerID: 0, Timestamp: 140,
	})
	// Up
	gr.HandleEvent(touch.Event{
		Type: touch.TouchTypeFinger, Action: touch.TouchActionUp,
		X: 0.7, Y: 0.7, PointerID: 0, Timestamp: 160,
	})

	if !rec.hasAction(ActionLeftDown) {
		t.Fatal("expected LeftDown to start drag")
	}
	if !rec.hasAction(ActionLeftDragged) {
		t.Fatal("expected LeftDragged during drag")
	}
	if !rec.hasAction(ActionLeftUp) {
		t.Fatal("expected LeftUp to end drag")
	}
}

func TestLongPressGeneratesRightClick(t *testing.T) {
	rec := &mockRecorder{}
	gr := NewGestureRecognizer(rec.handler)

	gr.HandleEvent(touch.Event{
		Type: touch.TouchTypeFinger, Action: touch.TouchActionDown,
		X: 0.5, Y: 0.5, PointerID: 0, Timestamp: 100,
	})

	// Wait for long press timer
	time.Sleep(600 * time.Millisecond)

	if !rec.hasAction(ActionRightDown) {
		t.Fatal("expected RightDown from long press")
	}
	if !rec.hasAction(ActionRightUp) {
		t.Fatal("expected RightUp from long press")
	}
}

func TestDoubleTapGeneratesDoubleClick(t *testing.T) {
	rec := &mockRecorder{}
	gr := NewGestureRecognizer(rec.handler)

	// First tap
	gr.HandleEvent(touch.Event{
		Type: touch.TouchTypeFinger, Action: touch.TouchActionDown,
		X: 0.5, Y: 0.5, PointerID: 0, Timestamp: 100,
	})
	gr.HandleEvent(touch.Event{
		Type: touch.TouchTypeFinger, Action: touch.TouchActionUp,
		X: 0.5, Y: 0.5, PointerID: 0, Timestamp: 150,
	})

	time.Sleep(100 * time.Millisecond)

	// Second tap
	gr.HandleEvent(touch.Event{
		Type: touch.TouchTypeFinger, Action: touch.TouchActionDown,
		X: 0.5, Y: 0.5, PointerID: 0, Timestamp: 300,
	})
	gr.HandleEvent(touch.Event{
		Type: touch.TouchTypeFinger, Action: touch.TouchActionUp,
		X: 0.5, Y: 0.5, PointerID: 0, Timestamp: 350,
	})

	time.Sleep(100 * time.Millisecond)

	// Double click = 2 down + 2 up
	downCount := rec.countAction(ActionLeftDown)
	if downCount != 2 {
		t.Fatalf("expected 2 LeftDown for double-click, got %d; events: %+v", downCount, rec.get())
	}
}

func TestTwoFingerScrollGeneratesScrollEvents(t *testing.T) {
	rec := &mockRecorder{}
	gr := NewGestureRecognizer(rec.handler)

	// Two fingers down
	gr.HandleEvent(touch.Event{
		Type: touch.TouchTypeFinger, Action: touch.TouchActionDown,
		X: 0.4, Y: 0.5, PointerID: 0, Timestamp: 100,
	})
	gr.HandleEvent(touch.Event{
		Type: touch.TouchTypeFinger, Action: touch.TouchActionDown,
		X: 0.6, Y: 0.5, PointerID: 1, Timestamp: 105,
	})

	// Both move up (scroll down in screen terms)
	gr.HandleEvent(touch.Event{
		Type: touch.TouchTypeFinger, Action: touch.TouchActionMove,
		X: 0.4, Y: 0.3, PointerID: 0, Timestamp: 120,
	})
	gr.HandleEvent(touch.Event{
		Type: touch.TouchTypeFinger, Action: touch.TouchActionMove,
		X: 0.6, Y: 0.3, PointerID: 1, Timestamp: 125,
	})

	// Fingers up
	gr.HandleEvent(touch.Event{
		Type: touch.TouchTypeFinger, Action: touch.TouchActionUp,
		X: 0.4, Y: 0.3, PointerID: 0, Timestamp: 140,
	})
	gr.HandleEvent(touch.Event{
		Type: touch.TouchTypeFinger, Action: touch.TouchActionUp,
		X: 0.6, Y: 0.3, PointerID: 1, Timestamp: 145,
	})

	if !rec.hasAction(ActionScroll) {
		t.Fatal("expected Scroll event from two-finger movement")
	}
}

func TestMoveWithinThresholdDoesNotDrag(t *testing.T) {
	rec := &mockRecorder{}
	gr := NewGestureRecognizer(rec.handler)

	gr.HandleEvent(touch.Event{
		Type: touch.TouchTypeFinger, Action: touch.TouchActionDown,
		X: 0.5, Y: 0.5, PointerID: 0, Timestamp: 100,
	})
	// Small move within threshold
	gr.HandleEvent(touch.Event{
		Type: touch.TouchTypeFinger, Action: touch.TouchActionMove,
		X: 0.505, Y: 0.505, PointerID: 0, Timestamp: 110,
	})
	gr.HandleEvent(touch.Event{
		Type: touch.TouchTypeFinger, Action: touch.TouchActionUp,
		X: 0.505, Y: 0.505, PointerID: 0, Timestamp: 120,
	})

	// Wait for tap timer
	time.Sleep(400 * time.Millisecond)

	// Should be a tap (click), NOT a drag
	if rec.hasAction(ActionLeftDragged) {
		t.Fatal("small move should not trigger drag")
	}
	if !rec.hasAction(ActionLeftDown) {
		t.Fatal("should be a tap/click")
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `cd /Users/luke/sideProject/android-mac/server && go test ./internal/input/ -v -run "TestTap|TestDrag|TestLong|TestDouble|TestTwo|TestMove"`
Expected: FAIL — types not found

**Step 3: Write the implementation**

Create `server/internal/input/gesture.go`:

```go
package input

import (
	"sync"
	"time"

	"github.com/luke/android-mac/server/internal/touch"
)

// MouseActionType represents the type of mouse event to inject.
type MouseActionType int

const (
	ActionMouseMove   MouseActionType = iota
	ActionLeftDown
	ActionLeftUp
	ActionLeftDragged
	ActionRightDown
	ActionRightUp
	ActionScroll
)

// MouseEvent is the output of the gesture recognizer.
type MouseEvent struct {
	Action  MouseActionType
	X, Y    float32 // normalized coordinates (passed through to injector)
	ScrollX int32   // pixel scroll delta X
	ScrollY int32   // pixel scroll delta Y
}

// MouseEventHandler is the callback for recognized gestures.
type MouseEventHandler func(MouseEvent)

type gestureState int

const (
	stateIdle gestureState = iota
	stateOneDown
	stateDragging
	stateTwoDown
	stateScrolling
	stateLongPress
)

const (
	longPressDelay = 500 * time.Millisecond
	doubleTapDelay = 300 * time.Millisecond
	dragThreshold  = float32(0.015) // 1.5% of screen
	scrollScale    = int32(800)     // multiply normalized delta to pixel delta
)

type pointerState struct {
	X, Y       float32
	DownX      float32
	DownY      float32
}

// GestureRecognizer converts raw touch events into mouse actions.
type GestureRecognizer struct {
	mu             sync.Mutex
	state          gestureState
	pointers       map[int32]*pointerState
	handler        MouseEventHandler
	longPressTimer *time.Timer
	tapPending     bool
	tapTimer       *time.Timer
	tapX, tapY     float32
}

// NewGestureRecognizer creates a recognizer that emits mouse events to handler.
func NewGestureRecognizer(handler MouseEventHandler) *GestureRecognizer {
	return &GestureRecognizer{
		pointers: make(map[int32]*pointerState),
		handler:  handler,
	}
}

// HandleEvent processes a touch event from the Android client.
func (gr *GestureRecognizer) HandleEvent(event touch.Event) {
	gr.mu.Lock()
	defer gr.mu.Unlock()

	switch event.Action {
	case touch.TouchActionDown:
		gr.handleDown(event)
	case touch.TouchActionMove:
		gr.handleMove(event)
	case touch.TouchActionUp:
		gr.handleUp(event)
	}
}

func (gr *GestureRecognizer) handleDown(e touch.Event) {
	gr.pointers[e.PointerID] = &pointerState{
		X: e.X, Y: e.Y,
		DownX: e.X, DownY: e.Y,
	}

	activeCount := len(gr.pointers)

	switch {
	case activeCount == 1:
		// Cancel pending tap timer (for double-tap detection)
		if gr.tapPending && gr.tapTimer != nil {
			gr.tapTimer.Stop()
			gr.tapPending = false
		}

		gr.state = stateOneDown

		// Start long press timer
		x, y := e.X, e.Y
		gr.longPressTimer = time.AfterFunc(longPressDelay, func() {
			gr.mu.Lock()
			defer gr.mu.Unlock()
			if gr.state == stateOneDown {
				gr.state = stateLongPress
				gr.handler(MouseEvent{Action: ActionRightDown, X: x, Y: y})
				gr.handler(MouseEvent{Action: ActionRightUp, X: x, Y: y})
			}
		})

	case activeCount >= 2:
		// Cancel long press for multi-touch
		if gr.longPressTimer != nil {
			gr.longPressTimer.Stop()
		}
		gr.state = stateTwoDown
	}
}

func (gr *GestureRecognizer) handleMove(e touch.Event) {
	p, ok := gr.pointers[e.PointerID]
	if !ok {
		return
	}

	oldX, oldY := p.X, p.Y
	p.X = e.X
	p.Y = e.Y

	switch gr.state {
	case stateOneDown:
		dx := e.X - p.DownX
		dy := e.Y - p.DownY
		dist := dx*dx + dy*dy
		if dist > dragThreshold*dragThreshold {
			if gr.longPressTimer != nil {
				gr.longPressTimer.Stop()
			}
			gr.state = stateDragging
			// Send mouse down at the original touch point, then drag to current
			gr.handler(MouseEvent{Action: ActionLeftDown, X: p.DownX, Y: p.DownY})
			gr.handler(MouseEvent{Action: ActionLeftDragged, X: e.X, Y: e.Y})
		}

	case stateDragging:
		gr.handler(MouseEvent{Action: ActionLeftDragged, X: e.X, Y: e.Y})

	case stateTwoDown:
		gr.state = stateScrolling
		fallthrough

	case stateScrolling:
		deltaX := int32(float32(scrollScale) * (e.X - oldX))
		deltaY := int32(float32(scrollScale) * (e.Y - oldY))
		if deltaX != 0 || deltaY != 0 {
			gr.handler(MouseEvent{Action: ActionScroll, ScrollX: deltaX, ScrollY: deltaY})
		}
	}
}

func (gr *GestureRecognizer) handleUp(e touch.Event) {
	delete(gr.pointers, e.PointerID)

	switch gr.state {
	case stateOneDown:
		if gr.longPressTimer != nil {
			gr.longPressTimer.Stop()
		}

		if gr.tapPending {
			// Second tap → double click
			gr.tapPending = false
			gr.handler(MouseEvent{Action: ActionMouseMove, X: e.X, Y: e.Y})
			gr.handler(MouseEvent{Action: ActionLeftDown, X: e.X, Y: e.Y})
			gr.handler(MouseEvent{Action: ActionLeftUp, X: e.X, Y: e.Y})
			gr.handler(MouseEvent{Action: ActionLeftDown, X: e.X, Y: e.Y})
			gr.handler(MouseEvent{Action: ActionLeftUp, X: e.X, Y: e.Y})
		} else {
			// First tap — wait for possible second tap
			gr.tapPending = true
			gr.tapX = e.X
			gr.tapY = e.Y
			gr.tapTimer = time.AfterFunc(doubleTapDelay, func() {
				gr.mu.Lock()
				defer gr.mu.Unlock()
				if gr.tapPending {
					gr.tapPending = false
					gr.handler(MouseEvent{Action: ActionMouseMove, X: gr.tapX, Y: gr.tapY})
					gr.handler(MouseEvent{Action: ActionLeftDown, X: gr.tapX, Y: gr.tapY})
					gr.handler(MouseEvent{Action: ActionLeftUp, X: gr.tapX, Y: gr.tapY})
				}
			})
		}

	case stateDragging:
		gr.handler(MouseEvent{Action: ActionLeftUp, X: e.X, Y: e.Y})

	case stateLongPress:
		// Long press already handled, just cleanup

	case stateScrolling, stateTwoDown:
		// If more pointers remain, stay in scroll mode
	}

	if len(gr.pointers) == 0 {
		gr.state = stateIdle
	}
}
```

**Step 4: Run tests to verify they pass**

Run: `cd /Users/luke/sideProject/android-mac/server && go test ./internal/input/ -v -run "TestTap|TestDrag|TestLong|TestDouble|TestTwo|TestMove" -count=1`
Expected: PASS — all 6 tests

**Step 5: Commit**

```bash
cd /Users/luke/sideProject/android-mac
git add server/internal/input/gesture.go server/internal/input/gesture_test.go
git commit -m "功能：新增手勢辨識器（點擊、拖曳、捲動、長按、雙擊）"
```

---

### Task 5: Mac Server Integration

**Files:**
- Modify: `server/internal/protocol/messages.go` — add `TouchPort` to `ServerHello`
- Modify: `server/internal/control/server.go` — pass touch port into ServerHello response
- Modify: `server/cmd/server/main.go` — start touch server, wire gesture recognizer + injector
- Modify: `server/internal/control/server_test.go` — update test for new TouchPort field

**Context:** Wire all touch components into the server main loop. The touch server starts on an auto-assigned port, which is communicated to the client via the ServerHello `touchPort` field. When a pipeline starts, the gesture recognizer and input injector are created for the virtual display's ID.

**Step 1: Update protocol messages**

In `server/internal/protocol/messages.go`, add `TouchPort` to `ServerHello`:

```go
type ServerHello struct {
	VirtualDisplay DisplayInfo `json:"virtualDisplay"`
	Codec          string      `json:"codec"`
	Bitrate        int         `json:"bitrate"`
	FPS            int         `json:"fps"`
	StreamPort     int         `json:"streamPort"`
	TouchPort      int         `json:"touchPort"`
}
```

**Step 2: Update control server to include touch port**

In `server/internal/control/server.go`:
- Add a `touchPort` field to `Server`
- Add `SetTouchPort(port int)` method
- Include `TouchPort` in the ServerHello response

**Step 3: Update main.go to start touch server and wire components**

In `server/cmd/server/main.go`:
- Import `touch` and `input` packages
- Start touch server on port 0 before control server
- Set touch port on control server via `SetTouchPort()`
- After virtual display is created in pipeline, create `input.NewInjector(displayID)`
- Create `input.NewGestureRecognizer()` that calls the injector
- Wire touch server events → gesture recognizer

**Step 4: Update server_test.go**

Update the handshake test to expect the new `TouchPort` field in ServerHello.

**Step 5: Run all tests**

Run: `cd /Users/luke/sideProject/android-mac/server && go test ./... -v`
Expected: PASS — all existing + updated tests

**Step 6: Verify build**

Run: `cd /Users/luke/sideProject/android-mac/server && go build ./cmd/server/`
Expected: builds successfully

**Step 7: Commit**

```bash
cd /Users/luke/sideProject/android-mac
git add server/internal/protocol/messages.go server/internal/control/server.go server/internal/control/server_test.go server/cmd/server/main.go
git commit -m "功能：整合觸控模組至 Mac 伺服器（觸控端口、手勢辨識、事件注入）"
```

---

### Task 6: Android Touch Support

**Files:**
- Create: `client/app/src/main/java/com/androidmac/client/touch/TouchEvent.kt`
- Create: `client/app/src/main/java/com/androidmac/client/touch/TouchSender.kt`
- Modify: `client/app/src/main/java/com/androidmac/client/protocol/Messages.kt` — add `touchPort` to ServerHello
- Modify: `client/app/src/main/java/com/androidmac/client/DisplayActivity.kt` — add touch listener + sender
- Modify: `client/app/src/main/java/com/androidmac/client/MainActivity.kt` — pass touchPort to DisplayActivity

**Context:** Android-side touch support. Captures finger events from the SurfaceView, serializes them to the 26-byte binary format (matching the Go server), and sends them over a TCP connection to the Mac touch port. Handles multi-touch for scroll (sends events for all active pointers on MOVE).

**Step 1: Create TouchEvent.kt**

```kotlin
package com.androidmac.client.touch

import java.nio.ByteBuffer
import java.nio.ByteOrder

data class TouchEvent(
    val type: Byte,
    val action: Byte,
    val x: Float,
    val y: Float,
    val pressure: Float,
    val pointerId: Int,
    val timestamp: Long
) {
    companion object {
        const val SIZE = 26
        const val TYPE_FINGER: Byte = 0
        const val TYPE_PEN: Byte = 1
        const val ACTION_DOWN: Byte = 0
        const val ACTION_MOVE: Byte = 1
        const val ACTION_UP: Byte = 2
    }

    fun toBytes(): ByteArray {
        val buf = ByteBuffer.allocate(SIZE).order(ByteOrder.BIG_ENDIAN)
        buf.put(type)
        buf.put(action)
        buf.putFloat(x)
        buf.putFloat(y)
        buf.putFloat(pressure)
        buf.putInt(pointerId)
        buf.putLong(timestamp)
        return buf.array()
    }
}
```

**Step 2: Create TouchSender.kt**

```kotlin
package com.androidmac.client.touch

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import java.io.BufferedOutputStream
import java.io.OutputStream
import java.net.Socket

class TouchSender {
    private var socket: Socket? = null
    private var output: OutputStream? = null

    suspend fun connect(host: String, port: Int) = withContext(Dispatchers.IO) {
        socket = Socket(host, port).also {
            it.tcpNoDelay = true
            output = BufferedOutputStream(it.getOutputStream())
        }
    }

    suspend fun send(event: TouchEvent) = withContext(Dispatchers.IO) {
        output?.let {
            it.write(event.toBytes())
            it.flush()
        }
    }

    fun disconnect() {
        try { socket?.close() } catch (_: Exception) {}
        socket = null
        output = null
    }
}
```

**Step 3: Update Messages.kt**

Add `touchPort` to `ServerHello`:

```kotlin
data class ServerHello(
    val virtualDisplay: DisplayInfo,
    val codec: String,
    val bitrate: Int,
    val fps: Int,
    val streamPort: Int,
    val touchPort: Int
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
                streamPort = obj.getInt("streamPort"),
                touchPort = obj.optInt("touchPort", 0)
            )
        }
    }
}
```

**Step 4: Update DisplayActivity.kt**

- Add `touchPort` and `serverHost` intent extras
- Create `TouchSender` and connect to touch port
- Set `onTouchListener` on `surfaceView` that captures multi-touch events:
  - DOWN/POINTER_DOWN → send for action pointer
  - MOVE → send for ALL active pointers
  - UP/POINTER_UP → send for action pointer
- Normalize coordinates to 0.0–1.0
- Detect TOOL_TYPE_FINGER vs TOOL_TYPE_STYLUS
- Clean up sender on destroy

**Step 5: Update MainActivity.kt**

- Pass `touchPort` and `serverHost` (IP) to DisplayActivity via intent extras

**Step 6: Verify Android build**

Run: `cd /Users/luke/sideProject/android-mac/client && ./gradlew assembleDebug`
Expected: BUILD SUCCESSFUL

**Step 7: Commit**

```bash
cd /Users/luke/sideProject/android-mac
git add client/app/src/main/java/com/androidmac/client/touch/TouchEvent.kt
git add client/app/src/main/java/com/androidmac/client/touch/TouchSender.kt
git add client/app/src/main/java/com/androidmac/client/protocol/Messages.kt
git add client/app/src/main/java/com/androidmac/client/DisplayActivity.kt
git add client/app/src/main/java/com/androidmac/client/MainActivity.kt
git commit -m "功能：新增 Android 觸控支援（事件擷取、二進位序列化、TCP 傳送）"
```

---

## Summary

| Task | Description | Test Strategy | Complexity |
|------|-------------|---------------|------------|
| 1 | Touch event binary protocol | Unit tests: marshal/unmarshal round-trip | ★☆☆☆☆ |
| 2 | Touch TCP server | Unit tests: TCP send/receive events | ★★☆☆☆ |
| 3 | CGEvent input injector | Build-only (CGo, requires real display) | ★★☆☆☆ |
| 4 | Gesture recognizer | Unit tests: tap, drag, scroll, long-press, double-tap | ★★★☆☆ |
| 5 | Mac server integration | Existing tests + build verification | ★★☆☆☆ |
| 6 | Android touch support | Build verification (./gradlew assembleDebug) | ★★☆☆☆ |

**Dependencies:**
- Task 2 depends on Task 1 (event types)
- Task 3 is independent (CGo bridge)
- Task 4 depends on Tasks 1 + 3 (event types + mouse event types in same package)
- Task 5 depends on Tasks 1–4 (wires everything)
- Task 6 depends on Task 5 (needs touchPort protocol)
