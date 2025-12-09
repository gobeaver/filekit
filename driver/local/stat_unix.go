//go:build unix

package local

import (
	"os"
	"strconv"
	"syscall"
	"time"

	"github.com/gobeaver/filekit"
)

// extractPlatformInfo extracts platform-specific file information on Unix systems.
func extractPlatformInfo(info os.FileInfo) (owner *filekit.FileOwner, createdAt *time.Time) {
	sys := info.Sys()
	if sys == nil {
		return nil, nil
	}

	stat, ok := sys.(*syscall.Stat_t)
	if !ok {
		return nil, nil
	}

	// Extract owner information (UID/GID)
	owner = &filekit.FileOwner{
		ID: strconv.FormatUint(uint64(stat.Uid), 10),
	}

	// Extract birth time (creation time) if available
	// On macOS, Birthtimespec is available
	// On Linux, this might be zero
	createdAt = extractBirthTime(stat)

	return owner, createdAt
}
