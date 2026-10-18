package tui

import "testing"

func TestFmtRemaining(t *testing.T) {
	for hours, want := range map[float64]string{
		4380: "182 days (6 months)",
		730:  "30 days (1 month)",
		729:  "30 days",
		47:   "1 day",
		10:   "0 days",
	} {
		if got := fmtRemaining(hours); got != want {
			t.Errorf("fmtRemaining(%v) = %q, want %q", hours, got, want)
		}
	}
}
