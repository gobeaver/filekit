//go:build linux

package local

import (
	"syscall"
	"time"
)

// extractBirthTime attempts to extract the birth time on Linux.
// Linux statx() supports birth time, but it's not always available
// and requires kernel 4.11+ and filesystem support.
// For simplicity, we return nil here as standard syscall.Stat_t
// doesn't include birth time on Linux.
func extractBirthTime(stat *syscall.Stat_t) *time.Time {
	// Linux doesn't expose birth time in standard Stat_t
	// Would need to use statx() syscall for newer kernels
	return nil
}
