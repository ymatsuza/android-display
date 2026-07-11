package adb

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Manager handles ADB device detection and reverse port forwarding
// for USB connections.
type Manager struct {
	adbPath string
}

// NewManager creates an ADB manager by finding the adb binary.
// Searches: PATH, ~/Library/Android/sdk, ~/Android/sdk, and project platform-tools.
func NewManager() (*Manager, error) {
	// Try PATH first
	if path, err := exec.LookPath("adb"); err == nil {
		return &Manager{adbPath: path}, nil
	}

	// Try common SDK locations
	home, _ := os.UserHomeDir()
	candidates := []string{
		filepath.Join(home, "Library", "Android", "sdk", "platform-tools", "adb"),
		filepath.Join(home, "Android", "sdk", "platform-tools", "adb"),
	}

	// Try ANDROID_SDK_ROOT / ANDROID_HOME
	for _, env := range []string{"ANDROID_SDK_ROOT", "ANDROID_HOME"} {
		if root := os.Getenv(env); root != "" {
			candidates = append(candidates, filepath.Join(root, "platform-tools", "adb"))
		}
	}

	for _, path := range candidates {
		if _, err := os.Stat(path); err == nil {
			return &Manager{adbPath: path}, nil
		}
	}

	return nil, fmt.Errorf("adb not found in PATH or common locations")
}

// Path returns the resolved adb binary path.
func (m *Manager) Path() string {
	return m.adbPath
}

// HasDevice returns true if at least one ADB device is connected.
func (m *Manager) HasDevice() bool {
	serials, err := m.ListDeviceSerials()
	if err != nil {
		return false
	}
	return len(serials) > 0
}

// ListDeviceSerials returns the serials of all currently attached ADB devices
// in "device" state (excludes "unauthorized"/"offline"/etc).
func (m *Manager) ListDeviceSerials() ([]string, error) {
	out, err := exec.Command(m.adbPath, "devices").Output()
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	var serials []string
	// First line is "List of devices attached", actual devices follow
	for _, line := range lines[1:] {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[1] == "device" {
			serials = append(serials, fields[0])
		}
	}
	return serials, nil
}

// SetupReverse creates a reverse port forwarding on the given device: Android localhost:port → Mac localhost:port.
// This allows the Android app to connect to localhost:port and reach the Mac server.
func (m *Manager) SetupReverse(serial string, port int) error {
	portStr := fmt.Sprintf("tcp:%d", port)
	cmd := exec.Command(m.adbPath, "-s", serial, "reverse", portStr, portStr)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("adb -s %s reverse %s failed: %v (%s)", serial, portStr, err, strings.TrimSpace(string(out)))
	}
	log.Printf("adb -s %s reverse %s → %s", serial, portStr, portStr)
	return nil
}

// HasReverse reports whether the given device currently has a reverse
// forwarding rule listening on tcp:port. Reverse forwards live in the
// device's adbd and silently vanish when the USB link drops, so callers
// polling for device presence must also verify the forwards survived.
func (m *Manager) HasReverse(serial string, port int) (bool, error) {
	out, err := exec.Command(m.adbPath, "-s", serial, "reverse", "--list").Output()
	if err != nil {
		return false, fmt.Errorf("adb -s %s reverse --list failed: %w", serial, err)
	}
	return reverseListHas(string(out), port), nil
}

// reverseListHas parses `adb reverse --list` output. Each line looks like
// "UsbFfs tcp:9000 tcp:9000" — the first field is the transport name (not
// the serial), the second is the device-side listen spec.
func reverseListHas(out string, port int) bool {
	portStr := fmt.Sprintf("tcp:%d", port)
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 3 && fields[1] == portStr {
			return true
		}
	}
	return false
}

// RemoveReverse removes a single reverse forwarding rule on the given device.
func (m *Manager) RemoveReverse(serial string, port int) error {
	portStr := fmt.Sprintf("tcp:%d", port)
	cmd := exec.Command(m.adbPath, "-s", serial, "reverse", "--remove", portStr)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("adb -s %s reverse --remove %s failed: %v (%s)", serial, portStr, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// RemoveAllReverse removes all reverse forwarding rules on the given device.
func (m *Manager) RemoveAllReverse(serial string) error {
	cmd := exec.Command(m.adbPath, "-s", serial, "reverse", "--remove-all")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("adb -s %s reverse --remove-all failed: %v (%s)", serial, err, strings.TrimSpace(string(out)))
	}
	log.Printf("adb -s %s reverse --remove-all", serial)
	return nil
}
