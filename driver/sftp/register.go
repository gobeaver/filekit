package sftp

import (
	"fmt"
	"os"

	"github.com/gobeaver/filekit"
)

func init() {
	filekit.RegisterDriver("sftp", func(cfg *filekit.Config) (filekit.FileSystem, error) {
		if cfg.SFTPHost == "" {
			return nil, fmt.Errorf("SFTP host is required")
		}

		sftpConfig := Config{
			Host:     cfg.SFTPHost,
			Port:     cfg.SFTPPort,
			Username: cfg.SFTPUsername,
			Password: cfg.SFTPPassword,
			BasePath: cfg.SFTPBasePath,
		}

		// Load private key if specified
		if cfg.SFTPPrivateKey != "" {
			keyData, err := os.ReadFile(cfg.SFTPPrivateKey)
			if err != nil {
				return nil, fmt.Errorf("failed to read private key: %w", err)
			}
			sftpConfig.PrivateKey = keyData
		}

		return New(sftpConfig)
	})
}
