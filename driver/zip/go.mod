module github.com/gobeaver/filekit/driver/zip

go 1.24.0

toolchain go1.24.2

require github.com/gobeaver/filekit v0.0.0

require (
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/gobeaver/beaver-kit/config v0.1.0 // indirect
	github.com/gobeaver/filekit/filevalidator v0.0.0 // indirect
)

replace github.com/gobeaver/filekit => ../..

replace github.com/gobeaver/filekit/filevalidator => ../../filevalidator
