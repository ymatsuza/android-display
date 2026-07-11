package adb

import "testing"

func TestReverseListHas(t *testing.T) {
	// Real `adb reverse --list` output (captured 2026-07-11): the first
	// field is the transport name (e.g. "UsbFfs"), not the serial, and the
	// output ends with a blank line.
	out := "UsbFfs tcp:9000 tcp:9000\n\n"
	if !reverseListHas(out, 9000) {
		t.Fatal("expected tcp:9000 to be found")
	}
	if reverseListHas(out, 9001) {
		t.Fatal("tcp:9001 must not be found")
	}
	if reverseListHas("", 9000) {
		t.Fatal("empty listing must report missing")
	}
	multi := "UsbFfs tcp:9000 tcp:9000\nUsbFfs tcp:9100 tcp:9100\n\n"
	if !reverseListHas(multi, 9100) {
		t.Fatal("expected tcp:9100 in multi-line listing")
	}
	if reverseListHas("UsbFfs tcp:19000 tcp:19000\n", 9000) {
		t.Fatal("tcp:19000 must not match tcp:9000 (substring false positive)")
	}
}
