package azure

import (
	"fmt"

	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"github.com/gobeaver/filekit"
)

func init() {
	filekit.RegisterDriver("azure", func(cfg *filekit.Config) (filekit.FileSystem, error) {
		if cfg.AzureAccountName == "" || cfg.AzureAccountKey == "" {
			return nil, fmt.Errorf("azure account name and key are required")
		}

		if cfg.AzureContainerName == "" {
			return nil, fmt.Errorf("azure container name is required")
		}

		// Build service URL
		serviceURL := fmt.Sprintf("https://%s.blob.core.windows.net/", cfg.AzureAccountName)
		if cfg.AzureEndpoint != "" {
			serviceURL = cfg.AzureEndpoint
		}

		// Create shared key credential
		cred, err := azblob.NewSharedKeyCredential(cfg.AzureAccountName, cfg.AzureAccountKey)
		if err != nil {
			return nil, fmt.Errorf("failed to create azure credential: %w", err)
		}

		// Create client
		client, err := azblob.NewClientWithSharedKeyCredential(serviceURL, cred, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to create azure client: %w", err)
		}

		var options []AdapterOption
		if cfg.AzurePrefix != "" {
			options = append(options, WithPrefix(cfg.AzurePrefix))
		}

		return New(client, cfg.AzureContainerName, cfg.AzureAccountName, cfg.AzureAccountKey, options...), nil
	})
}
