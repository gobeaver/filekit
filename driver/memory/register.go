package memory

import "github.com/gobeaver/filekit"

func init() {
	filekit.RegisterDriver("memory", func(cfg *filekit.Config) (filekit.FileSystem, error) {
		return New(), nil
	})
}
