package pullpreview

import (
	"testing"
	"time"
)

func TestPollAttemptsForWindow(t *testing.T) {
	tests := []struct {
		name     string
		window   time.Duration
		interval time.Duration
		want     int
	}{
		{
			name:     "full five minute window at five second cadence",
			window:   5 * time.Minute,
			interval: 5 * time.Second,
			want:     61,
		},
		{
			name:     "rounds up partial interval windows",
			window:   301 * time.Second,
			interval: 5 * time.Second,
			want:     62,
		},
		{
			name:     "non-positive window falls back to one attempt",
			window:   0,
			interval: 5 * time.Second,
			want:     1,
		},
		{
			name:     "non-positive interval falls back to one attempt",
			window:   5 * time.Minute,
			interval: 0,
			want:     1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := pollAttemptsForWindow(tt.window, tt.interval); got != tt.want {
				t.Fatalf("pollAttemptsForWindow(%s, %s) = %d, want %d", tt.window, tt.interval, got, tt.want)
			}
		})
	}
}
