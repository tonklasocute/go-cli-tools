package reqping

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestWatchRunsRoundsUntilCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var outputs []string
	err := Watch(ctx, time.Millisecond, func(context.Context) error {
		outputs = append(outputs, "round")
		if len(outputs) == 2 {
			cancel()
		}
		return nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if len(outputs) != 2 {
		t.Fatalf("rounds = %d, want 2", len(outputs))
	}
}

func TestWatchStopsOnRoundError(t *testing.T) {
	boom := errors.New("boom")
	calls := 0
	err := Watch(context.Background(), time.Millisecond, func(context.Context) error {
		calls++
		if calls == 3 {
			return boom
		}
		return nil
	})
	if !errors.Is(err, boom) || calls != 3 {
		t.Fatalf("err = %v, calls = %d", err, calls)
	}
	// Immediate failure on the first round.
	if err := Watch(context.Background(), time.Hour, func(context.Context) error { return boom }); !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
}

func TestWatchAlreadyCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	called := false
	err := Watch(ctx, time.Millisecond, func(context.Context) error { called = true; return nil })
	if !errors.Is(err, context.Canceled) || called {
		t.Fatalf("err = %v, called = %v", err, called)
	}
}

func TestFormatInterval(t *testing.T) {
	tests := map[time.Duration]string{
		5 * time.Second:                  "5s",
		time.Minute:                      "1m",
		90 * time.Second:                 "1m30s",
		2 * time.Hour:                    "2h",
		500 * time.Millisecond:           "500ms",
		time.Hour + 30*time.Minute:       "1h30m",
		time.Hour + 30*time.Minute + 5e9: "1h30m5s",
	}
	for d, want := range tests {
		if got := formatInterval(d); got != want {
			t.Errorf("formatInterval(%v) = %q, want %q", d, got, want)
		}
	}
}
