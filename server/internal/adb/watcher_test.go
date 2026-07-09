package adb

import (
	"errors"
	"testing"
	"time"
)

func TestWatcherDetectsNewSerial(t *testing.T) {
	serials := []string{"A"}
	var seen []string
	w := NewWatcher(
		func() ([]string, error) { return serials, nil },
		[]string{"A"},
		func(s string) { seen = append(seen, s) },
	)

	w.poll()
	if len(seen) != 0 {
		t.Fatalf("initial serial must not trigger onNew, got %v", seen)
	}

	serials = []string{"A", "B"}
	w.poll()
	if len(seen) != 1 || seen[0] != "B" {
		t.Fatalf("expected onNew for B only, got %v", seen)
	}

	w.poll()
	if len(seen) != 1 {
		t.Fatalf("unchanged listing must not re-trigger onNew, got %v", seen)
	}
}

func TestWatcherReplugTriggersAgain(t *testing.T) {
	serials := []string{"A"}
	var seen []string
	w := NewWatcher(
		func() ([]string, error) { return serials, nil },
		[]string{"A"},
		func(s string) { seen = append(seen, s) },
	)

	serials = nil // unplugged
	w.poll()
	serials = []string{"A"} // replugged
	w.poll()
	if len(seen) != 1 || seen[0] != "A" {
		t.Fatalf("replugged serial must trigger onNew again, got %v", seen)
	}
}

func TestWatcherListErrorKeepsKnownSet(t *testing.T) {
	fail := true
	var seen []string
	w := NewWatcher(
		func() ([]string, error) {
			if fail {
				return nil, errors.New("adb server not running")
			}
			return []string{"A"}, nil
		},
		[]string{"A"},
		func(s string) { seen = append(seen, s) },
	)

	w.poll() // transient error: must not count as "all devices disappeared"
	fail = false
	w.poll() // A was known all along → no onNew
	if len(seen) != 0 {
		t.Fatalf("listing error must not reset known set, got %v", seen)
	}
}

func TestWatcherRunStops(t *testing.T) {
	w := NewWatcher(
		func() ([]string, error) { return nil, nil },
		nil,
		func(string) {},
	)
	done := make(chan struct{})
	go func() {
		w.Run(time.Millisecond)
		close(done)
	}()
	time.Sleep(5 * time.Millisecond)
	w.Stop()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Run did not return after Stop")
	}
}
