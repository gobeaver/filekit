module github.com/gobeaver/filekit/driver/local

go 1.24.0

toolchain go1.24.2

require (
	github.com/fsnotify/fsnotify v1.9.0
	github.com/gobeaver/filekit v0.0.0
)

require (
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/gobeaver/beaver-kit/config v0.1.0 // indirect
	github.com/gobeaver/filekit/filevalidator v0.0.0 // indirect
	golang.org/x/sys v0.13.0 // indirect
)

// During development, use local replace directives
replace github.com/gobeaver/filekit => ../..

replace github.com/gobeaver/filekit/filevalidator => ../../filevalidator
