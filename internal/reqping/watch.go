package reqping

import (
	"context"
	"strings"
	"time"
)

// Watch calls round immediately and then once per interval until ctx is
// cancelled or round returns an error. A round that takes longer than the
// interval delays the next one instead of overlapping it. Watch always
// returns a non-nil error: round's error, or ctx.Err() after cancellation.
func Watch(ctx context.Context, interval time.Duration, round func(context.Context) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := round(ctx); err != nil {
		return err
	}
	tk := time.NewTicker(interval)
	defer tk.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-tk.C:
			// Both channels may be ready; never start a round after cancel.
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := round(ctx); err != nil {
				return err
			}
		}
	}
}

// formatInterval renders d compactly: 5s, 1m, 1m30s, 500ms.
func formatInterval(d time.Duration) string {
	s := d.String()
	if strings.HasSuffix(s, "m0s") {
		s = strings.TrimSuffix(s, "0s")
	}
	if strings.HasSuffix(s, "h0m") {
		s = strings.TrimSuffix(s, "0m")
	}
	return s
}
