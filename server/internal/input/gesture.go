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
	dragThreshold  = float32(0.005) // 0.5% of screen (~10px) — smaller dead zone for snappier drag
	scrollScale    = int32(800)     // multiply normalized delta to pixel delta
)

type pointerState struct {
	X, Y  float32
	DownX float32
	DownY float32
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

// Close stops any pending timers owned by the recognizer.
func (gr *GestureRecognizer) Close() {
	gr.mu.Lock()
	defer gr.mu.Unlock()
	if gr.longPressTimer != nil {
		gr.longPressTimer.Stop()
	}
	if gr.tapTimer != nil {
		gr.tapTimer.Stop()
	}
}

// HandleEvent processes a touch event from the Android client.
// The mutex is released before calling the handler so CGEvent injection
// does not block the next touch event from being processed.
func (gr *GestureRecognizer) HandleEvent(event touch.Event) {
	gr.mu.Lock()
	var events [5]MouseEvent
	n := 0

	switch event.Action {
	case touch.TouchActionDown:
		n = gr.handleDown(event, events[:])
	case touch.TouchActionMove:
		n = gr.handleMove(event, events[:])
	case touch.TouchActionUp:
		n = gr.handleUp(event, events[:])
	}

	gr.mu.Unlock()

	// Emit events outside the lock — CGEvent injection can be slow
	for i := 0; i < n; i++ {
		gr.handler(events[i])
	}
}

// handleDown returns events to emit (written into out). Returns count.
func (gr *GestureRecognizer) handleDown(e touch.Event, out []MouseEvent) int {
	gr.pointers[e.PointerID] = &pointerState{
		X: e.X, Y: e.Y,
		DownX: e.X, DownY: e.Y,
	}

	activeCount := len(gr.pointers)

	switch {
	case activeCount == 1:
		// Cancel pending tap timer (for double-tap detection).
		// Keep tapPending=true so handleUp knows this is the second tap.
		if gr.tapPending && gr.tapTimer != nil {
			gr.tapTimer.Stop()
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

	return 0
}

// handleMove returns events to emit. Returns count.
func (gr *GestureRecognizer) handleMove(e touch.Event, out []MouseEvent) int {
	p, ok := gr.pointers[e.PointerID]
	if !ok {
		return 0
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
			out[0] = MouseEvent{Action: ActionLeftDown, X: p.DownX, Y: p.DownY}
			out[1] = MouseEvent{Action: ActionLeftDragged, X: e.X, Y: e.Y}
			return 2
		}
		// 閾值內也先送 MouseMove，讓游標即時跟手
		out[0] = MouseEvent{Action: ActionMouseMove, X: e.X, Y: e.Y}
		return 1

	case stateDragging:
		out[0] = MouseEvent{Action: ActionLeftDragged, X: e.X, Y: e.Y}
		return 1

	case stateTwoDown:
		gr.state = stateScrolling
		fallthrough

	case stateScrolling:
		deltaX := int32(float32(scrollScale) * (e.X - oldX))
		deltaY := int32(float32(scrollScale) * (e.Y - oldY))
		if deltaX != 0 || deltaY != 0 {
			out[0] = MouseEvent{Action: ActionScroll, ScrollX: deltaX, ScrollY: deltaY}
			return 1
		}
	}

	return 0
}

// handleUp returns events to emit. Returns count.
func (gr *GestureRecognizer) handleUp(e touch.Event, out []MouseEvent) int {
	delete(gr.pointers, e.PointerID)
	n := 0

	switch gr.state {
	case stateOneDown:
		if gr.longPressTimer != nil {
			gr.longPressTimer.Stop()
		}

		if gr.tapPending {
			// Second tap -> double click
			gr.tapPending = false
			out[0] = MouseEvent{Action: ActionMouseMove, X: e.X, Y: e.Y}
			out[1] = MouseEvent{Action: ActionLeftDown, X: e.X, Y: e.Y}
			out[2] = MouseEvent{Action: ActionLeftUp, X: e.X, Y: e.Y}
			out[3] = MouseEvent{Action: ActionLeftDown, X: e.X, Y: e.Y}
			out[4] = MouseEvent{Action: ActionLeftUp, X: e.X, Y: e.Y}
			n = 5
		} else {
			// First tap -- wait for possible second tap
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
		out[0] = MouseEvent{Action: ActionLeftUp, X: e.X, Y: e.Y}
		n = 1

	case stateLongPress:
		// Already handled

	case stateScrolling, stateTwoDown:
		// Continue if more pointers remain
	}

	if len(gr.pointers) == 0 {
		gr.state = stateIdle
	}

	return n
}
