package humanize

import (
	"time"

	"github.com/dustin/go-humanize"
)

// Time returns a human-readable relative time string for times within the last 5 hours,
// or a formatted time string for older dates.
func Time(t time.Time) string {
	now := time.Now()
	duration := now.Sub(t)

	if duration > 5*time.Hour {
		return t.Format("Jan 2, 2006 15:04")
	}

	return humanize.Time(t)
}
