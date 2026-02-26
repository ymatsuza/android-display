# Android-Mac Extended Display + Touch Screen

## Overview

A custom software that turns a Samsung Tab S6 Lite (Android) into a wireless extended display + touch screen for macOS, with S Pen pressure support for drawing.

**Target Device**: Samsung Tab S6 Lite (2000x1200, 224 DPI, S Pen 4096 levels)
**Connection**: WiFi (5GHz preferred)
**Tech Stack**: Go core (Mac server) + Kotlin (Android client)

## Architecture

```
Mac (Go Server)                         Android (Kotlin Client)
┌──────────────────────┐    WiFi/UDP    ┌──────────────────────┐
│ VirtualDisplay       │───────────────▶│ VideoReceiver (UDP)  │
│ (CGVirtualDisplay)   │                │                      │
│                      │                │ MediaCodec H.264     │
│ ScreenCapture        │                │ Decode               │
│ (ScreenCaptureKit)   │                │                      │
│                      │                │ SurfaceView          │
│ H.264/H.265 Encoder  │                │ (fullscreen display) │
│ (VideoToolbox)       │                │                      │
│                      │◀───────────────│ TouchCollector       │
│ InputInjector        │    TCP         │ (finger + S Pen)     │
│ (CGEvent / HID)      │                │                      │
│                      │                │ TouchSender (TCP)    │
│ mDNS Service         │   mDNS        │ NSD Discovery        │
│ (Bonjour)            │◀─────────────▶│                      │
└──────────────────────┘                └──────────────────────┘
```

## Protocol Design

| Channel       | Protocol | Direction        | Purpose                          |
|---------------|----------|------------------|----------------------------------|
| Video Stream  | UDP      | Mac → Android    | Low-latency video transmission   |
| Touch Events  | TCP      | Android → Mac    | Reliable touch/pen data          |
| Control       | TCP      | Bidirectional    | Handshake, config, heartbeat     |
| Discovery     | mDNS     | LAN broadcast    | Auto-discover Mac from Android   |

### Video Stream Packet Format (UDP)

```
[Sequence 4B][Timestamp 8B][FrameType 1B][NAL Data...]
```

- Supports NAL unit splitting for large frames
- Optional FEC for packet loss recovery

### Touch Event Format (protobuf over TCP)

```protobuf
message TouchEvent {
  enum Type { FINGER = 0; PEN = 1; }
  enum Action { DOWN = 0; MOVE = 1; UP = 2; }

  Type type = 1;
  Action action = 2;
  float x = 3;           // 0.0 - 1.0 normalized
  float y = 4;           // 0.0 - 1.0 normalized
  float pressure = 5;    // 0.0 - 1.0 (S Pen only)
  int32 pointer_id = 6;  // multi-touch identifier
  int64 timestamp = 7;   // milliseconds
}
```

### Handshake Protocol (TCP)

```
Android → Mac:
  {device, screen: {w, h, dpi}, capabilities: ["touch","pen","pressure"], codec: ["h264","h265"]}

Mac → Android:
  {virtualDisplay: {w, h}, codec, bitrate, fps, streamPort}
```

## Mac Server Modules (Go + CGo)

### 1. VirtualDisplay

- CGo calls to `CGVirtualDisplay` (private API)
- Creates extended display matching Tab S6 Lite resolution (2000x1200)
- Registered as system extended display (not mirror)
- References: macos-virtual-display, node-mac-virtual-display

### 2. ScreenCapture

- CGo calls to `ScreenCaptureKit` (macOS 12.3+)
- Captures virtual display frames as CVPixelBuffer/IOSurface
- Variable frame rate: lower for static content, higher for dynamic

### 3. Encoder

- CGo calls to `VideoToolbox` (hardware H.264/H.265)
- Adaptive bitrate: 2-15 Mbps based on WiFi quality
- Keyframe interval: 1-2 seconds
- Low-latency / realtime encoding mode

### 4. Streaming (UDP)

- Custom packet format with sequence numbers and timestamps
- NAL unit splitting to avoid oversized UDP packets
- Optional Forward Error Correction (FEC)

### 5. InputInjector

- Phase 2: CGEvent mouse events (click, drag, scroll)
- Phase 3: Trackpad gesture injection (pinch, swipe)
- Phase 4: Virtual HID tablet device (DriverKit/IOKit) for pressure

### 6. mDNS Service

- Advertise service type `_androidmac._tcp` via Bonjour
- Go `net` package or `github.com/hashicorp/mdns`

## Android Client Modules (Kotlin)

### 1. Discovery

- Android NSD (NsdManager) scanning for `_androidmac._tcp`
- Display found Mac servers in list
- Fallback: manual IP input

### 2. VideoReceiver

- UDP packet reception → reassemble NAL units
- Jitter buffer for reordering
- Drop strategy: drop B/P frames, keep I frames when behind

### 3. Video Decoder

- MediaCodec hardware decode (Exynos 9611 supports H.264/H.265)
- Direct output to SurfaceView (zero-copy rendering)
- Fullscreen immersive mode, hide system UI

### 4. TouchCollector

- Override `View.onTouchEvent()` and `View.onGenericMotionEvent()`
- Distinguish `TOOL_TYPE_FINGER` vs `TOOL_TYPE_STYLUS`
- Collect: x, y, pressure, pointer_id, historical points
- Normalize coordinates to 0.0-1.0

### 5. TouchSender

- Protobuf serialization over TCP
- Batch MOVE events (5-10ms window)
- Rate limit to ~120Hz

### 6. UI

- Minimal: scan page → device list → fullscreen display
- Edge-swipe floating button for disconnect/settings

## Development Phases

### Phase 1: Screen Streaming (Mac → Android display)

**Goal**: Mac virtual display visible on Android tablet over WiFi

- Mac: VirtualDisplay + ScreenCapture + Encoder + UDP streaming
- Android: Discovery + Handshake + VideoReceiver + Decoder + SurfaceView
- **Deliverable**: Can see Mac extended desktop on tablet, drag windows to it
- **Complexity**: ★★★☆☆

### Phase 2: Basic Touch

**Goal**: Finger touch on Android controls Mac

- Android: TouchCollector + TouchSender
- Mac: InputInjector (CGEvent mouse events)
- Supported: tap→click, drag, two-finger scroll, long-press→right-click, double-tap
- **Deliverable**: Usable touch interaction with Mac extended display
- **Complexity**: ★★☆☆☆

### Phase 3: Advanced Gestures

**Goal**: macOS trackpad gestures via touch

- Android: Multi-finger gesture recognition
- Mac: Trackpad gesture event injection (private API or virtual trackpad HID)
- Supported: pinch-zoom, rotate, three-finger swipe (Mission Control / desktop switch)
- **Deliverable**: Touch experience close to native trackpad
- **Complexity**: ★★★★☆

### Phase 4: S Pen Pressure

**Goal**: S Pen works in Mac drawing apps with pressure sensitivity

- Android: TOOL_TYPE_STYLUS detection, pressure/tilt collection
- Mac: Virtual HID tablet device (DriverKit) emulating Wacom protocol
- Alternative: CGTabletProximity/CGTabletPoint event injection
- **Deliverable**: S Pen drawing with pressure-varying stroke width
- **Complexity**: ★★★★★

## Key Technical Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Language (Mac) | Go + CGo | Leverages existing Go experience; CGo bridges to macOS APIs |
| Language (Android) | Kotlin | Thin layer for MediaCodec + touch; AI-assisted development |
| Video codec | H.264 (default), H.265 (optional) | Hardware encode/decode on both sides; H.264 for compatibility |
| Video transport | UDP | Low latency; can tolerate packet loss |
| Touch transport | TCP | Reliable delivery for input events |
| Touch serialization | Protobuf | Compact, fast; better than JSON for high-frequency events |
| Virtual display | CGVirtualDisplay (private API) | Only way to create true extended display; used by all competitors |
| Device discovery | mDNS/Bonjour | Zero-config; native support on both platforms |

## Private API Risk Assessment

- `CGVirtualDisplay`: Private but stable since macOS 10.14, used by Duet/Luna/Side Screen
- Risk: May break on major macOS updates (annual check needed)
- Mitigation: Self-use only, no App Store requirement
- All competitor products share this same risk

## References

- [Side Screen](https://side-screen.vercel.app/) — MIT open source, USB only
- [macos-virtual-display](https://github.com/miolini/macos-virtual-display) — CGVirtualDisplay reference
- [node-mac-virtual-display](https://github.com/enfp-dev-studio/node-mac-virtual-display) — Another CGVirtualDisplay reference
- [scrcpy](https://github.com/Genymobile/scrcpy) — Reverse direction reference (Android→PC)
- [Deskreen](https://github.com/pavlobu/deskreen) — WebRTC-based screen sharing reference
