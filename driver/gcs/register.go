package gcs

import (
	"context"

	"cloud.google.com/go/storage"
	"github.com/gobeaver/filekit"
)

func init() {
	filekit.RegisterDriver("gcs", func(cfg *filekit.Config) (filekit.FileSystem, error) {
		ctx := context.Background()

		// Create client - uses GOOGLE_APPLICATION_CREDENTIALS env var or default credentials
		client, err := storage.NewClient(ctx)
		if err != nil {
			return nil, err
		}

		var options []AdapterOption
		if cfg.GCSPrefix != "" {
			options = append(options, WithPrefix(cfg.GCSPrefix))
		}

		return New(client, cfg.GCSBucket, options...), nil
	})
}
