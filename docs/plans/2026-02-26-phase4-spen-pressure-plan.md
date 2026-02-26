# Phase 4: S Pen Pressure Support Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** S Pen works in Mac drawing apps (Procreate, Photoshop, etc.) with pressure-sensitive strokes and tilt support.

**Architecture:** Extend the existing touch protocol with tilt fields (34 bytes), add CGTabletProximity/CGTabletPoint event injection on Mac via CGEvent API, and route pen events directly to the tablet injector bypassing the gesture recognizer. Android already detects S Pen and sends pressure — we add tilt collection.

**Tech Stack:** Go (CGo for CGEvent tablet APIs), Kotlin (Android MotionEvent AXIS_TILT/AXIS_ORIENTATION)

**Key Insight:** Phase 2 already sends `pressure` and `TOOL_TYPE_STYLUS` from Android. The Mac side just ignores it. Phase 4 makes the Mac side use this data via CGTabletPoint events that drawing apps understand.

**macOS Tablet Event Model:**
1. Send `CGTabletProximity` event when pen enters/leaves proximity (pen DOWN/UP)
2. Send mouse events with tablet subtype (`kCGEventMouseSubtypeTabletPoint`) and pressure/tilt fields
3. Drawing apps read pressure from `kCGTabletEventPointPressure` (0–65535 range)
4. Drawing apps read tilt from `kCGTabletEventTiltX/Y` (-1.0–1.0 range)

**Accessibility permission required** (same as Phase 2).

---

### Task 1: Extend Touch Event Protocol with Tilt

**Files:**
- Modify: `server/internal/touch/event.go`
- Modify: `server/internal/touch/event_test.go`

**Context:** Add TiltX and TiltY float32 fields to the binary protocol. This changes EventSize from 26 to 34 bytes. Both Go and Kotlin sides must update simultaneously.

**Changes to `event.go`:**

Update the layout comment, EventSize, Event struct, Marshal, and Unmarshal:

```go
// EventSize is the fixed byte size of a serialized touch event.
//
// Layout (34 bytes, big-endian):
//   Byte  0:     Type      (uint8)
//   Byte  1:     Action    (uint8)
//   Bytes 2-5:   X         (float32)
//   Bytes 6-9:   Y         (float32)
//   Bytes 10-13: Pressure  (float32)
//   Bytes 14-17: TiltX     (float32) — pen tilt left/right, -1.0 to 1.0
//   Bytes 18-21: TiltY     (float32) — pen tilt forward/back, -1.0 to 1.0
//   Bytes 22-25: PointerID (int32)
//   Bytes 26-33: Timestamp (int64, milliseconds)
const EventSize = 34

type Event struct {
	Type      TouchType
	Action    TouchAction
	X         float32
	Y         float32
	Pressure  float32
	TiltX     float32  // NEW: -1.0 to 1.0 (pen only, 0 for finger)
	TiltY     float32  // NEW: -1.0 to 1.0 (pen only, 0 for finger)
	PointerID int32
	Timestamp int64
}
```

Marshal adds TiltX at bytes 14-17, TiltY at 18-21, shifts PointerID to 22-25, Timestamp to 26-33.
Unmarshal reads in the same order.

**Update tests:** Fix expected EventSize to 34. Add a test for tilt values round-trip.

**Step 1:** Update event.go with new fields and serialization
**Step 2:** Update event_test.go
**Step 3:** Run tests: `go test ./internal/touch/ -v`
**Step 4:** Commit: `git commit -m "功能：擴展觸控協定新增傾斜欄位（34 字節）"`

---

### Task 2: CGTablet Event Injection (CGo Bridge)

**Files:**
- Modify: `server/bridge/input_bridge.h`
- Modify: `server/internal/input/input_bridge.m`
- Modify: `server/internal/input/injector.go`

**Context:** Add CGTabletProximity and CGTabletPoint event injection to the existing CGo bridge. Drawing apps need:
1. A proximity event announcing the pen
2. Mouse events with tablet subtype and pressure/tilt data attached

**Add to `input_bridge.h`:**

```c
// Tablet proximity — call when pen enters/leaves detection range
void InjectTabletProximityEnter(void);
void InjectTabletProximityLeave(void);

// Tablet point — mouse events with pressure and tilt for drawing apps
// pressure: 0.0-1.0, tiltX/tiltY: -1.0 to 1.0
void InjectTabletDown(double x, double y, double pressure, double tiltX, double tiltY);
void InjectTabletUp(double x, double y, double pressure, double tiltX, double tiltY);
void InjectTabletDragged(double x, double y, double pressure, double tiltX, double tiltY);
void InjectTabletMoved(double x, double y, double pressure, double tiltX, double tiltY);
```

**Add to `input_bridge.m`:**

Tablet proximity events:
```objc
void InjectTabletProximityEnter(void) {
    CGEventRef event = CGEventCreate(NULL);
    CGEventSetType(event, kCGEventTabletProximity);
    CGEventSetIntegerValueField(event, kCGTabletProximityEventVendorID, 0x056A); // Wacom vendor ID
    CGEventSetIntegerValueField(event, kCGTabletProximityEventTabletID, 1);
    CGEventSetIntegerValueField(event, kCGTabletProximityEventPointerID, 1);
    CGEventSetIntegerValueField(event, kCGTabletProximityEventDeviceID, 1);
    CGEventSetIntegerValueField(event, kCGTabletProximityEventSystemTabletID, 1);
    CGEventSetIntegerValueField(event, kCGTabletProximityEventVendorPointerType, 1); // pen
    CGEventSetIntegerValueField(event, kCGTabletProximityEventVendorPointerSerialNumber, 1);
    CGEventSetIntegerValueField(event, kCGTabletProximityEventVendorUniqueID, 1);
    CGEventSetIntegerValueField(event, kCGTabletProximityEventCapabilityMask, 0x00FE);
    CGEventSetIntegerValueField(event, kCGTabletProximityEventPointerType, 1); // pen
    CGEventSetIntegerValueField(event, kCGTabletProximityEventEnterProximity, 1);
    CGEventPost(kCGHIDEventTap, event);
    CFRelease(event);
}
```

Tablet point events (helper pattern):
```objc
static void InjectTabletEvent(CGEventType eventType, double x, double y, double pressure, double tiltX, double tiltY) {
    CGEventRef event = CGEventCreateMouseEvent(NULL, eventType, CGPointMake(x, y), kCGMouseButtonLeft);
    if (event) {
        CGEventSetIntegerValueField(event, kCGMouseEventSubtype, kCGEventMouseSubtypeTabletPoint);
        CGEventSetIntegerValueField(event, kCGTabletEventPointPressure, (int64_t)(pressure * 65535.0));
        CGEventSetDoubleValueField(event, kCGTabletEventTiltX, tiltX);
        CGEventSetDoubleValueField(event, kCGTabletEventTiltY, tiltY);
        CGEventPost(kCGHIDEventTap, event);
        CFRelease(event);
    }
}
```

Then InjectTabletDown calls `InjectTabletEvent(kCGEventLeftMouseDown, ...)`, etc.

**Add to `injector.go`:**

```go
func (inj *Injector) TabletProximityEnter() {
	C.InjectTabletProximityEnter()
}

func (inj *Injector) TabletProximityLeave() {
	C.InjectTabletProximityLeave()
}

func (inj *Injector) TabletDown(nx, ny float32, pressure float32, tiltX, tiltY float32) {
	x, y := inj.ToScreenCoords(nx, ny)
	C.InjectTabletDown(C.double(x), C.double(y), C.double(pressure), C.double(tiltX), C.double(tiltY))
}

func (inj *Injector) TabletUp(nx, ny float32, pressure float32, tiltX, tiltY float32) {
	x, y := inj.ToScreenCoords(nx, ny)
	C.InjectTabletUp(C.double(x), C.double(y), C.double(pressure), C.double(tiltX), C.double(tiltY))
}

func (inj *Injector) TabletDragged(nx, ny float32, pressure float32, tiltX, tiltY float32) {
	x, y := inj.ToScreenCoords(nx, ny)
	C.InjectTabletDragged(C.double(x), C.double(y), C.double(pressure), C.double(tiltX), C.double(tiltY))
}

func (inj *Injector) TabletMoved(nx, ny float32, pressure float32, tiltX, tiltY float32) {
	x, y := inj.ToScreenCoords(nx, ny)
	C.InjectTabletMoved(C.double(x), C.double(y), C.double(pressure), C.double(tiltX), C.double(tiltY))
}
```

**Step 1:** Update bridge header, ObjC implementation, Go wrapper
**Step 2:** Verify build: `go build ./internal/input/`
**Step 3:** Commit: `git commit -m "功能：新增 CGTablet 壓感/傾斜事件注入橋接"`

---

### Task 3: Pen Event Routing (Mac Server)

**Files:**
- Modify: `server/cmd/server/main.go`

**Context:** Currently all touch events go through the gesture recognizer. For pen events, we want direct 1:1 mapping to tablet events (no gesture recognition). Route based on `event.Type`:
- `TouchTypeFinger` → gesture recognizer (existing flow)
- `TouchTypePen` → direct tablet injection

**Changes to `main.go`:**

Replace the touch event handler wiring:

```go
// Wire touch events → gesture recognizer (finger) or tablet injector (pen)
touchServer.OnEvent(func(e touch.Event) {
    if e.Type == touch.TouchTypePen {
        // Pen events bypass gesture recognition — direct tablet injection
        switch e.Action {
        case touch.TouchActionDown:
            injector.TabletProximityEnter()
            injector.TabletDown(e.X, e.Y, e.Pressure, e.TiltX, e.TiltY)
        case touch.TouchActionMove:
            injector.TabletDragged(e.X, e.Y, e.Pressure, e.TiltX, e.TiltY)
        case touch.TouchActionUp:
            injector.TabletUp(e.X, e.Y, e.Pressure, e.TiltX, e.TiltY)
            injector.TabletProximityLeave()
        }
    } else {
        gesture.HandleEvent(e)
    }
})
```

**Step 1:** Update main.go event routing
**Step 2:** Verify build: `go build ./cmd/server/`
**Step 3:** Run all tests: `go test ./... -v`
**Step 4:** Commit: `git commit -m "功能：S Pen 事件路由（繞過手勢辨識，直接注入平板事件）"`

---

### Task 4: Android Tilt Collection

**Files:**
- Modify: `client/app/src/main/java/com/androidmac/client/touch/TouchEvent.kt`
- Modify: `client/app/src/main/java/com/androidmac/client/DisplayActivity.kt`

**Context:** The Android client already sends pressure and pen type. We need to:
1. Extend TouchEvent.kt to include tiltX/tiltY (34 bytes)
2. Collect tilt data from MotionEvent for S Pen

Samsung Tab S6 Lite S Pen provides:
- `MotionEvent.AXIS_TILT` — radial tilt angle in radians (0 = perpendicular)
- `MotionEvent.AXIS_ORIENTATION` — azimuth direction in radians

Convert to macOS tiltX/tiltY (-1.0 to 1.0):
```kotlin
val tilt = event.getAxisValue(MotionEvent.AXIS_TILT, index)
val orientation = event.getAxisValue(MotionEvent.AXIS_ORIENTATION, index)
val tiltX = (sin(orientation.toDouble()) * sin(tilt.toDouble())).toFloat()
val tiltY = (cos(orientation.toDouble()) * sin(tilt.toDouble())).toFloat()
```

For finger touches, tiltX and tiltY are 0.0.

**Changes to `TouchEvent.kt`:**
- Add `tiltX: Float` and `tiltY: Float` fields
- Update SIZE to 34
- Update `toBytes()` to serialize tiltX/tiltY between pressure and pointerId

**Changes to `DisplayActivity.kt`:**
- In `sendTouchEvent()`, collect tilt for stylus:
```kotlin
val (tiltX, tiltY) = if (event.getToolType(index) == MotionEvent.TOOL_TYPE_STYLUS) {
    val tilt = event.getAxisValue(MotionEvent.AXIS_TILT, index)
    val orientation = event.getAxisValue(MotionEvent.AXIS_ORIENTATION, index)
    Pair(
        (sin(orientation.toDouble()) * sin(tilt.toDouble())).toFloat(),
        (cos(orientation.toDouble()) * sin(tilt.toDouble())).toFloat()
    )
} else {
    Pair(0f, 0f)
}
```

**Step 1:** Update TouchEvent.kt
**Step 2:** Update DisplayActivity.kt
**Step 3:** Verify build: `./gradlew assembleDebug` (if available)
**Step 4:** Commit: `git commit -m "功能：Android S Pen 傾斜資料收集（AXIS_TILT + AXIS_ORIENTATION）"`

---

## Summary

| Task | Description | Changes | Complexity |
|------|-------------|---------|------------|
| 1 | Extend touch protocol with tilt | Go: event.go + tests | ★☆☆☆☆ |
| 2 | CGTablet event injection bridge | CGo: bridge + injector | ★★★☆☆ |
| 3 | Pen event routing in server | Go: main.go | ★☆☆☆☆ |
| 4 | Android tilt collection | Kotlin: TouchEvent + DisplayActivity | ★★☆☆☆ |

**Dependencies:** Task 2 and 3 depend on Task 1. Task 4 is independent (Kotlin side). Tasks 2+3 can be merged if desired.

**What we DON'T need to do:**
- ❌ New protocol connection (reuses existing touch TCP)
- ❌ New Android permissions (already have touch handling)
- ❌ DriverKit virtual HID (CGEvent tablet injection is simpler and sufficient)
- ❌ Change gesture recognizer (pen events bypass it entirely)
