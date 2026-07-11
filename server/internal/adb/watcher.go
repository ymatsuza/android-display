package adb

import "time"

// Watcher polls an ADB device listing and reports serials that newly appear
// (plugged in for the first time, replugged after a USB drop, or newly
// authorized). Reverse forwards live in the device's adbd and vanish when the
// USB link drops, so every appearance needs its forwards re-established.
type Watcher struct {
	list   func() ([]string, error)
	onNew  func(serial string)
	ensure func(serial string)
	known  map[string]struct{}
	stop   chan struct{}
}

// NewWatcher creates a watcher. Serials in initial are considered already
// handled and do not trigger onNew unless they disappear and come back.
func NewWatcher(list func() ([]string, error), initial []string, onNew func(serial string)) *Watcher {
	known := make(map[string]struct{}, len(initial))
	for _, s := range initial {
		known[s] = struct{}{}
	}
	return &Watcher{
		list:  list,
		onNew: onNew,
		known: known,
		stop:  make(chan struct{}),
	}
}

// EnsureKnown registers a callback invoked every poll for serials that were
// already known. A USB drop and reconnect completing between two polls never
// leaves the known set, yet its reverse forwards died with the link — the
// callback lets the caller verify and repair them.
func (w *Watcher) EnsureKnown(f func(serial string)) {
	w.ensure = f
}

// poll runs one detection cycle. A listing error leaves the known set
// untouched so a transient adb failure doesn't re-trigger onNew for devices
// that never actually disconnected.
func (w *Watcher) poll() {
	serials, err := w.list()
	if err != nil {
		return
	}
	current := make(map[string]struct{}, len(serials))
	for _, s := range serials {
		current[s] = struct{}{}
		if _, ok := w.known[s]; !ok {
			w.onNew(s)
		} else if w.ensure != nil {
			w.ensure(s)
		}
	}
	w.known = current
}

// Run polls at the given interval until Stop is called.
func (w *Watcher) Run(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			w.poll()
		case <-w.stop:
			return
		}
	}
}

// Stop terminates Run.
func (w *Watcher) Stop() {
	close(w.stop)
}
