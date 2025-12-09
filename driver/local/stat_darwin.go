//go:build darwin

package local

import (
	"syscall"
	"time"
)

// extractBirthTime extracts the birth time (creation time) on macOS.
func extractBirthTime(stat *syscall.Stat_t) *time.Time {
	// macOS has Birthtimespec for file creation time
	t := time.Unix(stat.Birthtimespec.Sec, stat.Birthtimespec.Nsec)
	if t.IsZero() {
		return nil
	}
	return &t
}
