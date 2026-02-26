package capture

/*
#cgo CFLAGS: -x objective-c -fobjc-arc -I../../bridge
#cgo LDFLAGS: -framework ScreenCaptureKit -framework CoreGraphics -framework CoreMedia -framework CoreVideo -framework Foundation
#include "../../bridge/capture_bridge.h"

extern void goFrameCallback(void *pixelBuffer, int width, int height, int64_t timestamp, void *userData);
*/
import "C"
import (
	"fmt"
	"log"
	"sync"
	"time"
	"unsafe"
)

// Frame represents a single captured screen frame containing a reference
// to the underlying CVPixelBuffer and its metadata.
type Frame struct {
	PixelBuffer unsafe.Pointer // CVPixelBufferRef — caller must call ReleasePixelBuffer when done
	Width       int
	Height      int
	Timestamp   int64 // presentation timestamp in microseconds
}

// FrameHandler is a callback function invoked for each captured frame.
// The handler must call ReleasePixelBuffer on frame.PixelBuffer when
// processing is complete.
type FrameHandler func(Frame)

var (
	handlerMu sync.Mutex
	handlers  = make(map[uintptr]FrameHandler)
	nextID    uintptr
)

func registerHandler(fn FrameHandler) uintptr {
	handlerMu.Lock()
	defer handlerMu.Unlock()
	nextID++
	handlers[nextID] = fn
	return nextID
}

func unregisterHandler(id uintptr) {
	handlerMu.Lock()
	defer handlerMu.Unlock()
	delete(handlers, id)
}

//export goFrameCallback
func goFrameCallback(pixelBuffer unsafe.Pointer, width C.int, height C.int, timestamp C.int64_t, userData unsafe.Pointer) {
	id := uintptr(userData)
	handlerMu.Lock()
	fn, ok := handlers[id]
	handlerMu.Unlock()
	if ok {
		fn(Frame{
			PixelBuffer: pixelBuffer,
			Width:       int(width),
			Height:      int(height),
			Timestamp:   int64(timestamp),
		})
	}
}

// Capturer manages an active ScreenCaptureKit capture session.
type Capturer struct {
	stream    unsafe.Pointer
	handlerID uintptr
}

// Start begins capturing the specified display at the given frame rate.
// Each captured frame is delivered to handler asynchronously.
// The caller must call Stop when capture is no longer needed.
//
// For virtual displays, ScreenCaptureKit may need a moment to discover the
// new display. Start retries up to 3 times with 500ms delay between attempts.
func Start(displayID uint32, fps int, handler FrameHandler) (*Capturer, error) {
	id := registerHandler(handler)

	const maxRetries = 3
	var lastErr string
	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			log.Printf("capture: display %d not found, retrying (%d/%d)...", displayID, attempt+1, maxRetries)
			time.Sleep(500 * time.Millisecond)
		}

		result := C.StartCapture(
			C.CGDirectDisplayID(displayID),
			C.int(fps),
			(C.FrameCallback)(C.goFrameCallback),
			unsafe.Pointer(id),
		)

		if result.success != 0 {
			return &Capturer{
				stream:    result.stream,
				handlerID: id,
			}, nil
		}
		lastErr = C.GoString(&result.errorMsg[0])
	}

	unregisterHandler(id)
	return nil, fmt.Errorf("start capture: %s", lastErr)
}

// Stop terminates the capture session and releases all associated resources.
func (c *Capturer) Stop() {
	if c.stream != nil {
		C.StopCapture(c.stream)
		c.stream = nil
	}
	unregisterHandler(c.handlerID)
}

// ReleasePixelBuffer releases the CVPixelBufferRef obtained from a Frame.
// This must be called after the pixel data has been consumed (e.g., after
// encoding) to avoid memory leaks.
func ReleasePixelBuffer(buf unsafe.Pointer) {
	if buf != nil {
		C.ReleasePixelBuffer(buf)
	}
}
