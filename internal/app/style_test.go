package app

import (
	"testing"
	"time"
)

func TestShortDate(t *testing.T) {
	now := time.Date(2026, 10, 24, 15, 0, 0, 0, time.Local)
	for _, tc := range []struct {
		at   time.Time
		want string
	}{
		{time.Date(2026, 10, 24, 9, 5, 0, 0, time.Local), "09:05"},
		{time.Date(2026, 10, 23, 23, 0, 0, 0, time.Local), "Yesterday"},
		{time.Date(2026, 10, 19, 12, 0, 0, 0, time.Local), "Monday"},
		{time.Date(2026, 10, 17, 12, 0, 0, 0, time.Local), "Oct 17"},
		{time.Date(2025, 3, 1, 12, 0, 0, 0, time.Local), "2025/3/1"},
	} {
		if got := shortDate(tc.at, now); got != tc.want {
			t.Errorf("shortDate(%v) = %q, want %q", tc.at, got, tc.want)
		}
	}
}
