package touch

import (
	"math"
	"testing"
)

func TestEventMarshalRoundTrip(t *testing.T) {
	event := Event{
		Type: TouchTypeFinger, Action: TouchActionDown,
		X: 0.5, Y: 0.75, Pressure: 0.0, PointerID: 1, Timestamp: 1234567890,
	}
	buf := event.Marshal()
	if len(buf) != EventSize {
		t.Fatalf("expected %d bytes, got %d", EventSize, len(buf))
	}
	decoded := Unmarshal(buf)
	if decoded != event {
		t.Fatalf("round trip failed: got %+v, want %+v", decoded, event)
	}
}

func TestEventPenWithPressure(t *testing.T) {
	event := Event{
		Type: TouchTypePen, Action: TouchActionMove,
		X: 0.123, Y: 0.456, Pressure: 0.789, PointerID: 0, Timestamp: 9876543210,
	}
	buf := event.Marshal()
	decoded := Unmarshal(buf)
	if decoded.Type != TouchTypePen {
		t.Fatalf("expected PEN type, got %d", decoded.Type)
	}
	if math.Abs(float64(decoded.Pressure-0.789)) > 0.001 {
		t.Fatalf("pressure mismatch: got %f, want 0.789", decoded.Pressure)
	}
}

func TestEventSize(t *testing.T) {
	buf := (&Event{}).Marshal()
	if len(buf) != 26 {
		t.Fatalf("event should be 26 bytes, got %d", len(buf))
	}
}

func TestEventBoundaryValues(t *testing.T) {
	event := Event{
		Type: TouchTypeFinger, Action: TouchActionUp,
		X: 0.0, Y: 1.0, Pressure: 1.0, PointerID: -1, Timestamp: 0,
	}
	buf := event.Marshal()
	decoded := Unmarshal(buf)
	if decoded.X != 0.0 || decoded.Y != 1.0 {
		t.Fatalf("boundary values not preserved: x=%f y=%f", decoded.X, decoded.Y)
	}
	if decoded.PointerID != -1 {
		t.Fatalf("negative pointer ID not preserved: %d", decoded.PointerID)
	}
}
