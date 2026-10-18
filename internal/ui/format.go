package ui

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

var sizeUnits = []string{"B", "KiB", "MiB", "GiB", "TiB", "PiB", "EiB"}

// HumanBytes formats n bytes using binary (IEC) units, e.g. "1.5 MiB".
func HumanBytes(n int64) string {
	if n < 0 {
		return "-" + HumanBytes(-n)
	}
	if n < 1024 {
		return strconv.FormatInt(n, 10) + " B"
	}
	f := float64(n)
	i := 0
	for f >= 1024 && i < len(sizeUnits)-1 {
		f /= 1024
		i++
	}
	if f >= 100 {
		return fmt.Sprintf("%.0f %s", f, sizeUnits[i])
	}
	return fmt.Sprintf("%.1f %s", f, sizeUnits[i])
}

// ErrInvalidSize is returned by ParseSize for malformed input.
var ErrInvalidSize = errors.New("invalid size")

// ParseSize parses a human size such as "512", "10MB", "1.5GiB" or "200k".
//
// SI suffixes (KB, MB, GB, TB) are powers of 1000; IEC suffixes (KiB, MiB, …)
// and bare single letters (K, M, G, T) are powers of 1024, matching du(1).
func ParseSize(s string) (int64, error) {
	in := strings.TrimSpace(s)
	if in == "" {
		return 0, fmt.Errorf("%w: empty", ErrInvalidSize)
	}
	i := 0
	for i < len(in) && (in[i] >= '0' && in[i] <= '9' || in[i] == '.') {
		i++
	}
	num, unit := in[:i], strings.ToUpper(strings.TrimSpace(in[i:]))
	f, err := strconv.ParseFloat(num, 64)
	if err != nil || f < 0 {
		return 0, fmt.Errorf("%w: %q", ErrInvalidSize, s)
	}
	mult := map[string]float64{
		"": 1, "B": 1,
		"K": 1 << 10, "KIB": 1 << 10, "KB": 1e3,
		"M": 1 << 20, "MIB": 1 << 20, "MB": 1e6,
		"G": 1 << 30, "GIB": 1 << 30, "GB": 1e9,
		"T": 1 << 40, "TIB": 1 << 40, "TB": 1e12,
	}
	m, ok := mult[unit]
	if !ok {
		return 0, fmt.Errorf("%w: unknown unit %q", ErrInvalidSize, unit)
	}
	v := f * m
	if v > math.MaxInt64 {
		return 0, fmt.Errorf("%w: %q overflows", ErrInvalidSize, s)
	}
	return int64(v), nil
}

// HumanDuration formats d compactly with sensible precision:
// "850µs", "12ms", "1.24s", "2m05s".
func HumanDuration(d time.Duration) string {
	switch {
	case d < 0:
		return "-" + HumanDuration(-d)
	case d < time.Microsecond:
		return fmt.Sprintf("%dns", d.Nanoseconds())
	case d < time.Millisecond:
		return fmt.Sprintf("%dµs", d.Microseconds())
	case d < 10*time.Millisecond:
		return fmt.Sprintf("%.1fms", float64(d)/float64(time.Millisecond))
	case d < time.Second:
		return fmt.Sprintf("%dms", d.Milliseconds())
	case d < time.Minute:
		return fmt.Sprintf("%.2fs", d.Seconds())
	case d < time.Hour:
		m := d / time.Minute
		s := (d % time.Minute) / time.Second
		return fmt.Sprintf("%dm%02ds", m, s)
	default:
		h := d / time.Hour
		m := (d % time.Hour) / time.Minute
		return fmt.Sprintf("%dh%02dm", h, m)
	}
}

// Percent formats part/total as a percentage with one decimal ("12.5%").
// It returns "0.0%" when total is zero.
func Percent(part, total int64) string {
	if total <= 0 {
		return "0.0%"
	}
	return fmt.Sprintf("%.1f%%", float64(part)*100/float64(total))
}

// Plural returns "1 file" / "3 files" style strings.
func Plural(n int, singular, plural string) string {
	if n == 1 {
		return "1 " + singular
	}
	return strconv.Itoa(n) + " " + plural
}
