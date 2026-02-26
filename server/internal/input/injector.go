package input

/*
#cgo CFLAGS: -x objective-c -I../../bridge
#cgo LDFLAGS: -framework CoreGraphics -framework ApplicationServices
#include "input_bridge.h"
*/
import "C"

import "log"

// DisplayBounds represents the pixel bounds of a display.
type DisplayBounds struct {
	X, Y, Width, Height float64
}

// Injector translates normalised touch coordinates into macOS CGEvent
// mouse actions and posts them via the Quartz Event Services API.
type Injector struct {
	displayID uint32
	bounds    DisplayBounds
}

// NewInjector creates a new Injector targeting the given display.
// It logs a warning if Accessibility permission has not been granted.
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

// ToScreenCoords converts normalised (0..1) coordinates to absolute
// screen coordinates within the injector's display bounds.
func (inj *Injector) ToScreenCoords(nx, ny float32) (float64, float64) {
	x := inj.bounds.X + float64(nx)*inj.bounds.Width
	y := inj.bounds.Y + float64(ny)*inj.bounds.Height
	return x, y
}

// Bounds returns the display bounds used by this injector.
func (inj *Injector) Bounds() DisplayBounds {
	return inj.bounds
}

// MouseMove posts a mouse-moved event at the normalised coordinates.
func (inj *Injector) MouseMove(nx, ny float32) {
	x, y := inj.ToScreenCoords(nx, ny)
	C.InjectMouseMove(C.double(x), C.double(y))
}

// LeftMouseDown posts a left-mouse-down event at the normalised coordinates.
func (inj *Injector) LeftMouseDown(nx, ny float32) {
	x, y := inj.ToScreenCoords(nx, ny)
	C.InjectLeftMouseDown(C.double(x), C.double(y))
}

// LeftMouseUp posts a left-mouse-up event at the normalised coordinates.
func (inj *Injector) LeftMouseUp(nx, ny float32) {
	x, y := inj.ToScreenCoords(nx, ny)
	C.InjectLeftMouseUp(C.double(x), C.double(y))
}

// LeftMouseDragged posts a left-mouse-dragged event at the normalised coordinates.
func (inj *Injector) LeftMouseDragged(nx, ny float32) {
	x, y := inj.ToScreenCoords(nx, ny)
	C.InjectLeftMouseDragged(C.double(x), C.double(y))
}

// RightMouseDown posts a right-mouse-down event at the normalised coordinates.
func (inj *Injector) RightMouseDown(nx, ny float32) {
	x, y := inj.ToScreenCoords(nx, ny)
	C.InjectRightMouseDown(C.double(x), C.double(y))
}

// RightMouseUp posts a right-mouse-up event at the normalised coordinates.
func (inj *Injector) RightMouseUp(nx, ny float32) {
	x, y := inj.ToScreenCoords(nx, ny)
	C.InjectRightMouseUp(C.double(x), C.double(y))
}

// ScrollWheel posts a scroll-wheel event with the given pixel deltas.
func (inj *Injector) ScrollWheel(deltaX, deltaY int32) {
	C.InjectScrollWheel(C.int32_t(deltaX), C.int32_t(deltaY))
}
