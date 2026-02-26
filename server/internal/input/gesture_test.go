package input

import (
	"sync"
	"testing"
	"time"

	"github.com/luke/android-mac/server/internal/touch"
)

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

	gr.HandleEvent(touch.Event{
		Type: touch.TouchTypeFinger, Action: touch.TouchActionDown,
		X: 0.5, Y: 0.5, PointerID: 0, Timestamp: 100,
	})
	gr.HandleEvent(touch.Event{
		Type: touch.TouchTypeFinger, Action: touch.TouchActionMove,
		X: 0.6, Y: 0.6, PointerID: 0, Timestamp: 120,
	})
	gr.HandleEvent(touch.Event{
		Type: touch.TouchTypeFinger, Action: touch.TouchActionMove,
		X: 0.7, Y: 0.7, PointerID: 0, Timestamp: 140,
	})
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

	downCount := rec.countAction(ActionLeftDown)
	if downCount != 2 {
		t.Fatalf("expected 2 LeftDown for double-click, got %d; events: %+v", downCount, rec.get())
	}
}

func TestTwoFingerScrollGeneratesScrollEvents(t *testing.T) {
	rec := &mockRecorder{}
	gr := NewGestureRecognizer(rec.handler)

	gr.HandleEvent(touch.Event{
		Type: touch.TouchTypeFinger, Action: touch.TouchActionDown,
		X: 0.4, Y: 0.5, PointerID: 0, Timestamp: 100,
	})
	gr.HandleEvent(touch.Event{
		Type: touch.TouchTypeFinger, Action: touch.TouchActionDown,
		X: 0.6, Y: 0.5, PointerID: 1, Timestamp: 105,
	})

	gr.HandleEvent(touch.Event{
		Type: touch.TouchTypeFinger, Action: touch.TouchActionMove,
		X: 0.4, Y: 0.3, PointerID: 0, Timestamp: 120,
	})
	gr.HandleEvent(touch.Event{
		Type: touch.TouchTypeFinger, Action: touch.TouchActionMove,
		X: 0.6, Y: 0.3, PointerID: 1, Timestamp: 125,
	})

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
	gr.HandleEvent(touch.Event{
		Type: touch.TouchTypeFinger, Action: touch.TouchActionMove,
		X: 0.505, Y: 0.505, PointerID: 0, Timestamp: 110,
	})
	gr.HandleEvent(touch.Event{
		Type: touch.TouchTypeFinger, Action: touch.TouchActionUp,
		X: 0.505, Y: 0.505, PointerID: 0, Timestamp: 120,
	})

	time.Sleep(400 * time.Millisecond)

	if rec.hasAction(ActionLeftDragged) {
		t.Fatal("small move should not trigger drag")
	}
	if !rec.hasAction(ActionLeftDown) {
		t.Fatal("should be a tap/click")
	}
}
