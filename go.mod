module github.com/gobeaver/filekit

go 1.24.0

toolchain go1.24.2

require (
	github.com/cespare/xxhash/v2 v2.3.0
	github.com/gobeaver/beaver-kit/config v0.1.0
	github.com/gobeaver/filekit/driver/local v0.0.0-00010101000000-000000000000
	github.com/gobeaver/filekit/filevalidator v0.0.0
)

require (
	github.com/fsnotify/fsnotify v1.9.0 // indirect
	golang.org/x/sys v0.13.0 // indirect
)

// Local submodules
replace (
	github.com/gobeaver/filekit/driver/local => ./driver/local
	github.com/gobeaver/filekit/filevalidator => ./filevalidator
)

// Exclude all published beaver-kit module versions to avoid ambiguous imports
exclude (
	github.com/gobeaver/beaver-kit v0.0.1
	github.com/gobeaver/beaver-kit v0.0.2
	github.com/gobeaver/beaver-kit v0.0.3
	github.com/gobeaver/beaver-kit v0.0.4
	github.com/gobeaver/beaver-kit v0.1.0
	github.com/gobeaver/beaver-kit v0.1.1
)
