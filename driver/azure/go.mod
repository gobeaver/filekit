module github.com/gobeaver/filekit/driver/azure

go 1.24.0

toolchain go1.24.2

require (
	github.com/Azure/azure-sdk-for-go/sdk/azcore v1.20.0
	github.com/Azure/azure-sdk-for-go/sdk/storage/azblob v1.6.3
	github.com/gobeaver/filekit v0.0.0
)

require (
	github.com/Azure/azure-sdk-for-go/sdk/internal v1.11.2 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/gobeaver/beaver-kit/config v0.1.0 // indirect
	github.com/gobeaver/filekit/filevalidator v0.0.0 // indirect
	golang.org/x/net v0.43.0 // indirect
	golang.org/x/text v0.28.0 // indirect
)

replace github.com/gobeaver/filekit => ../..

replace github.com/gobeaver/filekit/filevalidator => ../../filevalidator
