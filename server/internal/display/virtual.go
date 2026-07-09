package display

/*
#cgo CFLAGS: -x objective-c -fobjc-arc -I../../bridge
#cgo LDFLAGS: -framework CoreGraphics -framework Foundation
#include "../../bridge/display_bridge.h"
*/
import "C"
import (
	"fmt"
	"sync"
	"sync/atomic"
	"unsafe"
)

// createMu serializes all calls into the CGVirtualDisplay private API.
// The framework crashed with SIGSEGV on a second concurrent
// CreateVirtualDisplay call even 4s after the first display had fully
// stabilized, so this is not just a startup race — Create/Destroy calls
// must never overlap.
var createMu sync.Mutex

// nextSerial hands out a unique per-instance identifier so each virtual
// display gets distinct descriptor identity fields (see display_bridge.m).
var nextSerial atomic.Int32

// VirtualDisplay represents a macOS virtual display created via the
// private CGVirtualDisplay API. The display appears as a real extended
// monitor to the system.
type VirtualDisplay struct {
	ptr       unsafe.Pointer
	displayID uint32
	width     int
	height    int
}

// Config holds the parameters for creating a virtual display.
type Config struct {
	Width  int
	Height int
	PPI    int
	HiDPI  bool
}

// New creates a new virtual display with the given configuration.
// The caller must call Close when the display is no longer needed.
func New(cfg Config) (*VirtualDisplay, error) {
	ccfg := C.VirtualDisplayConfig{
		width:  C.int(cfg.Width),
		height: C.int(cfg.Height),
		ppi:    C.int(cfg.PPI),
		serial: C.int(nextSerial.Add(1)),
	}
	if cfg.HiDPI {
		ccfg.hiDPI = 1
	}

	createMu.Lock()
	result := C.CreateVirtualDisplay(ccfg)
	createMu.Unlock()
	if result.success == 0 {
		return nil, fmt.Errorf("create virtual display: %s", C.GoString(&result.errorMsg[0]))
	}

	return &VirtualDisplay{
		ptr:       result.display,
		displayID: uint32(result.displayID),
		width:     cfg.Width,
		height:    cfg.Height,
	}, nil
}

// DisplayID returns the CGDirectDisplayID assigned to this virtual display.
func (d *VirtualDisplay) DisplayID() uint32 {
	return d.displayID
}

// Width returns the pixel width of the virtual display.
func (d *VirtualDisplay) Width() int {
	return d.width
}

// Height returns the pixel height of the virtual display.
func (d *VirtualDisplay) Height() int {
	return d.height
}

// Close destroys the virtual display and releases all associated resources.
func (d *VirtualDisplay) Close() {
	if d.ptr != nil {
		createMu.Lock()
		C.DestroyVirtualDisplay(d.ptr)
		createMu.Unlock()
		d.ptr = nil
	}
}
