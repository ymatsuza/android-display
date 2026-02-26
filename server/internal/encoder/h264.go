package encoder

/*
#cgo CFLAGS: -x objective-c -fobjc-arc -I../../bridge
#cgo LDFLAGS: -framework VideoToolbox -framework CoreMedia -framework CoreVideo -framework CoreFoundation -framework Foundation
#include "../../bridge/encoder_bridge.h"
#include <string.h>

extern void goEncodedCallback(uint8_t *nalData, int nalSize, int isKeyframe, int64_t timestamp, void *userData);

static inline int EncodeFrameFromPointer(void *session, void *pixelBuffer, int64_t timestamp) {
    return EncodeFrame(session, (CVPixelBufferRef)pixelBuffer, timestamp);
}
*/
import "C"
import (
	"fmt"
	"sync"
	"unsafe"
)

// nalBufPool recycles byte slices used to copy NAL data from C to Go,
// avoiding a heap allocation per NAL unit (~60+ per second at 60fps).
var nalBufPool = sync.Pool{
	New: func() any {
		buf := make([]byte, 0, 64*1024) // 64KB initial capacity
		return &buf
	},
}

// NALUnit represents a single H.264 NAL unit in Annex-B format
// (prefixed with 00 00 00 01 start code).
type NALUnit struct {
	Data       []byte
	IsKeyframe bool
	Timestamp  int64 // presentation timestamp in microseconds
	NALType    byte  // NAL unit type: 0x07=SPS, 0x08=PPS, 0x05=IDR, other=non-IDR
}

// NALHandler is a callback function invoked for each encoded NAL unit.
type NALHandler func(NALUnit)

var (
	nalHandlerMu sync.Mutex
	nalHandlers  = make(map[uintptr]NALHandler)
	nalNextID    uintptr
)

func registerNALHandler(fn NALHandler) uintptr {
	nalHandlerMu.Lock()
	defer nalHandlerMu.Unlock()
	nalNextID++
	nalHandlers[nalNextID] = fn
	return nalNextID
}

func unregisterNALHandler(id uintptr) {
	nalHandlerMu.Lock()
	defer nalHandlerMu.Unlock()
	delete(nalHandlers, id)
}

//export goEncodedCallback
func goEncodedCallback(nalData *C.uint8_t, nalSize C.int, isKeyframe C.int, timestamp C.int64_t, userData unsafe.Pointer) {
	id := uintptr(userData)
	nalHandlerMu.Lock()
	fn, ok := nalHandlers[id]
	nalHandlerMu.Unlock()
	if !ok {
		return
	}

	size := int(nalSize)
	// Reuse a pooled buffer to avoid per-NAL heap allocation
	bufp := nalBufPool.Get().(*[]byte)
	goData := *bufp
	if cap(goData) < size {
		goData = make([]byte, size)
	} else {
		goData = goData[:size]
	}
	C.memcpy(unsafe.Pointer(&goData[0]), unsafe.Pointer(nalData), C.size_t(size))

	// Parse NAL unit type from the first byte after the Annex-B start code
	// (00 00 00 01). The NAL type is in the lower 5 bits.
	var nalType byte
	if size > 4 {
		nalType = goData[4] & 0x1F
	}

	fn(NALUnit{
		Data:       goData,
		IsKeyframe: isKeyframe != 0,
		Timestamp:  int64(timestamp),
		NALType:    nalType,
	})

	// Return buffer to pool after handler has consumed the data.
	// The handler (SendFrame) copies data into its own TCP buffer,
	// so it's safe to recycle this slice now.
	*bufp = goData
	nalBufPool.Put(bufp)
}

// Encoder wraps a VideoToolbox H.264 hardware compression session.
type Encoder struct {
	session   unsafe.Pointer
	context   unsafe.Pointer
	handlerID uintptr
}

// Config specifies the parameters for creating a new H.264 encoder.
type Config struct {
	Width   int // frame width in pixels
	Height  int // frame height in pixels
	FPS     int // target frame rate
	Bitrate int // average bitrate in bits per second
}

// New creates a new H.264 hardware encoder with the given configuration.
// Each encoded NAL unit is delivered to handler asynchronously.
// The caller must call Close when the encoder is no longer needed.
func New(cfg Config, handler NALHandler) (*Encoder, error) {
	id := registerNALHandler(handler)

	result := C.CreateEncoder(
		C.int(cfg.Width),
		C.int(cfg.Height),
		C.int(cfg.FPS),
		C.int(cfg.Bitrate),
		(C.EncodedCallback)(C.goEncodedCallback),
		unsafe.Pointer(id),
	)

	if result.success == 0 {
		unregisterNALHandler(id)
		return nil, fmt.Errorf("create encoder: %s", C.GoString(&result.errorMsg[0]))
	}

	return &Encoder{
		session:   result.session,
		context:   result.context,
		handlerID: id,
	}, nil
}

// Encode submits a CVPixelBufferRef for H.264 encoding. The pixelBuffer
// parameter must be a valid CVPixelBufferRef (e.g., obtained from screen
// capture). The timestamp is the presentation time in microseconds.
func (e *Encoder) Encode(pixelBuffer unsafe.Pointer, timestamp int64) error {
	status := C.EncodeFrameFromPointer(e.session, pixelBuffer, C.int64_t(timestamp))
	if status != 0 {
		return fmt.Errorf("encode frame: status %d", status)
	}
	return nil
}

// Close flushes any pending frames, tears down the compression session,
// and releases all associated resources.
func (e *Encoder) Close() {
	if e.session != nil {
		C.DestroyEncoder(e.session, e.context)
		e.session = nil
		e.context = nil
	}
	unregisterNALHandler(e.handlerID)
}
