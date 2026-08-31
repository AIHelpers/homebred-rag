package watcher

import (
	"context"
	"time"
)

// PollWatcher watches a folder for changes by polling on an interval and
// invoking a callback. It's the dependency-free stand-in for `fsnotify`
// (plan section 5) — real fsnotify needs a `go get` this sandboxed build
// can't perform without network access to a Go module proxy, but it's a
// drop-in swap later: same "call onChange when something under root
// changed" contract, just event-driven instead of polled.
type PollWatcher struct {
	Root     string
	Interval time.Duration
	OnChange func()
}

func NewPollWatcher(root string, interval time.Duration, onChange func()) *PollWatcher {
	if interval <= 0 {
		interval = 5 * time.Second
	}
	return &PollWatcher{Root: root, Interval: interval, OnChange: onChange}
}

// Run blocks, polling until ctx is cancelled. Each tick just calls
// OnChange (typically a re-ingest of Root); the ingester's own hash-based
// change detection is what makes repeated calls cheap when nothing moved.
func (w *PollWatcher) Run(ctx context.Context) {
	ticker := time.NewTicker(w.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.OnChange()
		}
	}
}
