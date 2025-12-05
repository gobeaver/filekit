// Package filekit provides a unified filesystem abstraction for Go with support
// for multiple storage backends, encryption, validation, and virtual path mounting.
//
// FileKit follows interface segregation principles, providing separate interfaces
// for read-only ([FileReader]) and write ([FileWriter]) operations, combined in the
// full [FileSystem] interface. This allows compile-time enforcement of access patterns.
//
// # Storage Backends
//
// FileKit supports 7 storage backends through a multi-module architecture:
//
//   - Local filesystem (github.com/gobeaver/filekit/driver/local)
//   - Amazon S3 (github.com/gobeaver/filekit/driver/s3)
//   - Google Cloud Storage (github.com/gobeaver/filekit/driver/gcs)
//   - Azure Blob Storage (github.com/gobeaver/filekit/driver/azure)
//   - SFTP (github.com/gobeaver/filekit/driver/sftp)
//   - In-memory (github.com/gobeaver/filekit/driver/memory)
//   - ZIP archives (github.com/gobeaver/filekit/driver/zip)
//
// Each driver is a separate Go module, so you only pull dependencies for the
// backends you actually use.
//
// # Basic Usage
//
//	import "github.com/gobeaver/filekit/driver/local"
//
//	fs, err := local.New("./storage")
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	ctx := context.Background()
//
//	// Write a file
//	err = fs.Write(ctx, "hello.txt", strings.NewReader("Hello, World!"))
//
//	// Read a file
//	data, err := fs.ReadAll(ctx, "hello.txt")
//
//	// Check existence
//	exists, err := fs.FileExists(ctx, "hello.txt")
//
//	// List directory contents
//	files, err := fs.ListContents(ctx, "/", false)
//
// # Optional Capabilities
//
// Drivers may implement optional capability interfaces for advanced features.
// Use type assertions to check for support:
//
//	// Check for native copy support
//	if copier, ok := fs.(filekit.CanCopy); ok {
//	    err := copier.Copy(ctx, "source.txt", "dest.txt")
//	}
//
//	// Generate pre-signed URLs (cloud storage)
//	if signer, ok := fs.(filekit.CanSignURL); ok {
//	    url, err := signer.SignedURL(ctx, "file.pdf", 15*time.Minute)
//	}
//
//	// Calculate checksums
//	if cs, ok := fs.(filekit.CanChecksum); ok {
//	    hash, err := cs.Checksum(ctx, "file.txt", filekit.ChecksumSHA256)
//	}
//
//	// Watch for file changes
//	if watcher, ok := fs.(filekit.CanWatch); ok {
//	    token, err := watcher.Watch(ctx, "**/*.json")
//	    if token.HasChanged() {
//	        // Handle change
//	    }
//	}
//
// # Mount Manager
//
// The [MountManager] provides virtual path namespacing, allowing multiple storage
// backends to be combined under a unified path structure:
//
//	mounts := filekit.NewMountManager()
//	mounts.Mount("/local", localDriver)
//	mounts.Mount("/cloud", s3Driver)
//
//	// Transparent access - routes to correct backend
//	mounts.Write(ctx, "/local/file.txt", reader)
//	mounts.Read(ctx, "/cloud/image.png")
//
//	// Cross-mount operations work automatically
//	mounts.Copy(ctx, "/local/file.txt", "/cloud/backup/file.txt")
//
// # Decorators
//
// FileKit provides stackable decorators for cross-cutting concerns:
//
//	// Read-only protection
//	readOnly := filekit.NewReadOnlyFileSystem(fs)
//
//	// Metadata caching
//	cached := filekit.NewCachingFileSystem(fs,
//	    filekit.WithCacheTTL(5*time.Minute),
//	)
//
//	// Encryption (AES-256-GCM)
//	encrypted, err := filekit.NewEncryptedFS(fs, encryptionKey)
//
//	// File validation
//	validated := filekit.NewValidatedFileSystem(fs, validator)
//
// Decorators can be stacked in any order:
//
//	fs, _ = filekit.NewEncryptedFS(fs, key)
//	fs = filekit.NewValidatedFileSystem(fs, validator)
//	fs = filekit.NewCachingFileSystem(fs)
//	fs = filekit.NewReadOnlyFileSystem(fs)
//
// # File Selection
//
// The [FileSelector] interface enables flexible file filtering:
//
//	// Simple glob pattern
//	files, err := filekit.ListWithSelector(ctx, fs, "/", filekit.Glob("*.txt"), true)
//
//	// Composed selectors
//	selector := filekit.And(
//	    filekit.Glob("*.jpg"),
//	    filekit.FuncSelector(func(f *filekit.FileInfo) bool {
//	        return f.Size < 10*1024*1024 // Under 10MB
//	    }),
//	)
//	files, err := filekit.ListWithSelector(ctx, fs, "/images", selector, true)
//
// # Error Handling
//
// FileKit provides sentinel errors and helper functions for error handling:
//
//	_, err := fs.Read(ctx, "nonexistent.txt")
//	if filekit.IsNotExist(err) {
//	    // File does not exist
//	}
//
//	var pathErr *filekit.PathError
//	if errors.As(err, &pathErr) {
//	    fmt.Printf("Operation: %s, Path: %s\n", pathErr.Op, pathErr.Path)
//	}
//
// # Configuration
//
// FileKit can be configured via environment variables with the FILEKIT_ prefix,
// or programmatically via the [Config] struct:
//
//	cfg := filekit.Config{
//	    Driver:   "s3",
//	    S3Bucket: "my-bucket",
//	    S3Region: "us-west-2",
//	}
//	fs, err := filekit.New(cfg)
package filekit
