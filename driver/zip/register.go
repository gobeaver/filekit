package zip

import (
	"fmt"

	"github.com/gobeaver/filekit"
)

func init() {
	filekit.RegisterDriver("zip", func(cfg *filekit.Config) (filekit.FileSystem, error) {
		// ZIP driver requires a path - use LocalBasePath as the ZIP file path
		if cfg.LocalBasePath == "" {
			return nil, fmt.Errorf("zip driver requires LocalBasePath to be set to the ZIP file path")
		}

		return OpenOrCreate(cfg.LocalBasePath)
	})
}
