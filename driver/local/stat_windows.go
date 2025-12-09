//go:build windows

package local

import (
	"os"
	"syscall"
	"time"

	"github.com/gobeaver/filekit"
)

// extractPlatformInfo extracts platform-specific file information on Windows.
func extractPlatformInfo(info os.FileInfo) (owner *filekit.FileOwner, createdAt *time.Time) {
	sys := info.Sys()
	if sys == nil {
		return nil, nil
	}

	// On Windows, Sys() returns *syscall.Win32FileAttributeData
	data, ok := sys.(*syscall.Win32FileAttributeData)
	if !ok {
		return nil, nil
	}

	// Extract creation time (Windows has this natively)
	t := time.Unix(0, data.CreationTime.Nanoseconds())
	if !t.IsZero() {
		createdAt = &t
	}

	// Owner information requires additional Windows API calls (GetSecurityInfo)
	// which is complex, so we skip it for now
	return nil, createdAt
}
