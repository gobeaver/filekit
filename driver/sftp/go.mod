module github.com/gobeaver/filekit/driver/sftp

go 1.24.0

toolchain go1.24.2

require (
	github.com/gobeaver/filekit v0.0.0
	github.com/pkg/sftp v1.13.10
	golang.org/x/crypto v0.43.0
)

require (
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/gobeaver/beaver-kit/config v0.1.0 // indirect
	github.com/gobeaver/filekit/filevalidator v0.0.0 // indirect
	github.com/kr/fs v0.1.0 // indirect
	golang.org/x/sys v0.37.0 // indirect
)

replace github.com/gobeaver/filekit => ../..

replace github.com/gobeaver/filekit/filevalidator => ../../filevalidator
