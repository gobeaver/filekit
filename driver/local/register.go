package local

import "github.com/gobeaver/filekit"

func init() {
	filekit.RegisterDriver("local", func(cfg *filekit.Config) (filekit.FileSystem, error) {
		return New(cfg.LocalBasePath)
	})
}
