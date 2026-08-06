# android-display

Turn your Android tablet into a wireless extended display for macOS.

> **Note:** Touch and S Pen input injection is **currently disabled** in this fork — the tablet is display-only. See [Touch input (disabled)](#touch-input-disabled).

## Features

- **Wireless Extended Display** — Creates a real macOS virtual display on your Android tablet over WiFi
- **USB Mode** — Also supports USB connection via ADB reverse forwarding for lower latency
- **Auto Discovery** — mDNS/Bonjour zero-config, Android app finds your Mac automatically
- **Hardware Encoding** — H.264 via VideoToolbox (Mac) and MediaCodec (Android), low latency
- **Adjustable Quality** — 8M / 4M / 2M / 1M bitrate selector

## Architecture

```
Mac (Go + CGo Server)                    Android (Kotlin Client)
┌──────────────────────┐    WiFi/UDP    ┌──────────────────────┐
│ CGVirtualDisplay     │───────────────▶│ VideoReceiver        │
│ (private API)        │                │                      │
│                      │                │ MediaCodec H.264     │
│ CGDisplayStream      │                │ Decode               │
│ (screen capture)     │                │                      │
│                      │                │ SurfaceView          │
│ VideoToolbox H.264   │                │ (fullscreen display) │
│ (hw encode)          │                │                      │
│                      │◀───────────────│ TouchCollector       │
│ touch server         │    TCP         │ (finger + S Pen)     │
│ (events discarded)   │                │                      │
│                      │                │ TouchSender (TCP)    │
│ Bonjour mDNS         │   mDNS        │ NSD Discovery        │
│                      │◀─────────────▶│                      │
└──────────────────────┘                └──────────────────────┘
```

## Requirements

### Mac (Server)
- macOS 12.3+ (Monterey or later)
- Xcode Command Line Tools (`xcode-select --install`)
- Go 1.21+

### Android (Client)
- Android 10+ (API 29+)
- Android Studio (to build the APK)

## Build

### Mac Server

```bash
cd server
go build -o android-display-server ./cmd/server
```

### Android Client

Open the `client/` directory in Android Studio and build the APK, or:

```bash
cd client
./gradlew assembleDebug
```

Install the APK on your Android device:

```bash
adb install client/app/build/outputs/apk/debug/app-debug.apk
```

## Usage

1. **Start the Mac server:**
   ```bash
   ./server/android-display-server
   ```

2. **Open the Android app** on your tablet — it will auto-discover the Mac via mDNS

3. **Choose orientation** — select 横向き (landscape) or 縦向き (portrait), and optionally check 上下反転（180°） to flip the display upside-down (e.g. for mounting the tablet rotated)

4. **Tap Connect** — the tablet becomes an extended display

5. **Drag windows** from your Mac to the extended display

### USB Mode

For lower latency, connect your Android device via USB:

1. Enable USB debugging on your Android device
2. Connect via USB cable
3. Start the server (it will automatically set up ADB reverse forwarding)
4. Open the Android app, choose orientation, and connect

## Protocol

| Channel       | Protocol | Direction     | Purpose                    |
|---------------|----------|---------------|----------------------------|
| Video Stream  | UDP      | Mac → Android | Low-latency H.264 video    |
| Touch Events  | TCP      | Android → Mac | Touch/pen input — accepted, currently discarded (see below) |
| Control       | TCP      | Bidirectional | Handshake, config          |
| Discovery     | mDNS     | LAN broadcast | Auto-discover Mac server   |

## Touch input (disabled)

Touch and S Pen injection is commented out in `server/cmd/server/main.go` (`startPipelineCommon`). The tablet works as a display only.

Why: injection warps the macOS cursor with `CGWarpMouseCursorPosition`, and macOS has a single shared cursor across all displays. Every touch on the tablet therefore yanked the cursor away from whatever display was being used at the time.

What still happens: the client sends touch events exactly as before, and the server's touch port still listens and accepts them. With no handler registered they are read and dropped in `server/internal/touch/server.go`. The wire protocol and handshake are unchanged, so an already-installed APK keeps working — no reinstall needed.

To re-enable, uncomment two things in `server/cmd/server/main.go` and rebuild the server:

1. The injection block in `startPipelineCommon`
2. The `internal/input` import

## Known Limitations

- Uses `CGVirtualDisplay` private API — may break on future macOS updates
- WiFi 5GHz recommended for best streaming quality
- macOS may require Screen Recording permission for screen capture

## License

[Apache License 2.0](LICENSE)
