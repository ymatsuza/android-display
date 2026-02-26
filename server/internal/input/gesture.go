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
			// Second tap -> double click
			gr.tapPending = false
			gr.handler(MouseEvent{Action: ActionMouseMove, X: e.X, Y: e.Y})
			gr.handler(MouseEvent{Action: ActionLeftDown, X: e.X, Y: e.Y})
			gr.handler(MouseEvent{Action: ActionLeftUp, X: e.X, Y: e.Y})
			gr.handler(MouseEvent{Action: ActionLeftDown, X: e.X, Y: e.Y})
			gr.handler(MouseEvent{Action: ActionLeftUp, X: e.X, Y: e.Y})
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
		gr.handler(MouseEvent{Action: ActionLeftUp, X: e.X, Y: e.Y})

	case stateLongPress:
		// Already handled

	case stateScrolling, stateTwoDown:
		// Continue if more pointers remain
	}

	if len(gr.pointers) == 0 {
		gr.state = stateIdle
	}
}
