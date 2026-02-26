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
	out, err := exec.Command(m.adbPath, "devices").Output()
	if err != nil {
		return false
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	// First line is "List of devices attached", actual devices follow
	for _, line := range lines[1:] {
		line = strings.TrimSpace(line)
		if line != "" && strings.Contains(line, "\tdevice") {
			return true
		}
	}
	return false
}

// SetupReverse creates a reverse port forwarding: Android localhost:port → Mac localhost:port.
// This allows the Android app to connect to localhost:port and reach the Mac server.
func (m *Manager) SetupReverse(port int) error {
	portStr := fmt.Sprintf("tcp:%d", port)
	cmd := exec.Command(m.adbPath, "reverse", portStr, portStr)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("adb reverse %s failed: %v (%s)", portStr, err, strings.TrimSpace(string(out)))
	}
	log.Printf("adb reverse %s → %s", portStr, portStr)
	return nil
}

// RemoveReverse removes a single reverse forwarding rule.
func (m *Manager) RemoveReverse(port int) error {
	portStr := fmt.Sprintf("tcp:%d", port)
	cmd := exec.Command(m.adbPath, "reverse", "--remove", portStr)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("adb reverse --remove %s failed: %v (%s)", portStr, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// RemoveAllReverse removes all reverse forwarding rules.
func (m *Manager) RemoveAllReverse() error {
	cmd := exec.Command(m.adbPath, "reverse", "--remove-all")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("adb reverse --remove-all failed: %v (%s)", err, strings.TrimSpace(string(out)))
	}
	log.Println("adb reverse --remove-all")
	return nil
}
