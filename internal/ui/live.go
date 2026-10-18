package ui

import (
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// Live redraws a single status line (spinner or progress bar) on a terminal.
// It is a no-op when the target writer is not a terminal, so it is always
// safe to use — piped output and CI logs stay clean.
type Live struct {
	w        io.Writer
	th       *Theme
	label    string
	total    int64
	done     atomic.Int64
	enabled  bool
	interval time.Duration

	mu      sync.Mutex
	stop    chan struct{}
	stopped chan struct{}
	frame   int
}

// NewSpinner returns a spinner with the given label that draws on w.
func NewSpinner(w io.Writer, th *Theme, label string) *Live {
	return newLive(w, th, label, 0)
}

// NewProgress returns a progress bar for total units that draws on w.
func NewProgress(w io.Writer, th *Theme, label string, total int64) *Live {
	return newLive(w, th, label, total)
}

func newLive(w io.Writer, th *Theme, label string, total int64) *Live {
	return &Live{w: w, th: th, label: label, total: total, enabled: IsTerminal(w), interval: 80 * time.Millisecond}
}

// Enabled reports whether the indicator actually draws anything.
func (l *Live) Enabled() bool { return l.enabled }

// Add records n completed units of work.
func (l *Live) Add(n int64) { l.done.Add(n) }

// SetLabel replaces the label shown next to the indicator.
func (l *Live) SetLabel(s string) {
	l.mu.Lock()
	l.label = s
	l.mu.Unlock()
}

// Start begins redrawing in the background.
func (l *Live) Start() {
	if !l.enabled || l.stop != nil {
		return
	}
	l.stop = make(chan struct{})
	l.stopped = make(chan struct{})
	go func() {
		defer close(l.stopped)
		t := time.NewTicker(l.interval)
		defer t.Stop()
		l.draw()
		for {
			select {
			case <-l.stop:
				return
			case <-t.C:
				l.draw()
			}
		}
	}()
}

// Stop halts redrawing and clears the status line.
func (l *Live) Stop() {
	if l.stop == nil {
		return
	}
	close(l.stop)
	<-l.stopped
	l.stop = nil
	fmt.Fprint(l.w, "\r\x1b[2K")
}

func (l *Live) draw() {
	fmt.Fprint(l.w, "\r\x1b[2K"+l.Line())
}

// Line returns the current rendering of the indicator without a newline.
func (l *Live) Line() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	frame := spinnerFrames[l.frame%len(spinnerFrames)]
	l.frame++
	if l.total <= 0 {
		return l.th.Info(frame) + " " + l.label
	}
	done := l.done.Load()
	var b strings.Builder
	b.WriteString(l.th.Info(frame) + " ")
	if l.label != "" {
		b.WriteString(l.label + " ")
	}
	b.WriteString(ProgressBar(l.th, done, l.total, 30))
	b.WriteString(l.th.Muted(fmt.Sprintf("  %d/%d", done, l.total)))
	return b.String()
}
