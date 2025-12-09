package filekit

import (
	"context"
	"io"
	"time"
)

// FileInfo represents file or directory metadata returned by [FileReader.Stat]
// and [FileReader.ListContents].
type FileInfo struct {
	// Name is the base name of the file or directory (e.g., "photo.jpg").
	Name string

	// Path is the full path relative to the filesystem root (e.g., "images/photo.jpg").
	Path string

	// Size is the file size in bytes. For directories, this is typically 0.
	Size int64

	// ModTime is the last modification time of the file or directory.
	ModTime time.Time

	// IsDir is true if this entry represents a directory.
	IsDir bool

	// ContentType is the MIME type of the file (e.g., "image/jpeg").
	// May be empty if not detected or not applicable (directories).
	ContentType string

	// Metadata contains custom key-value metadata associated with the file.
	// Cloud storage backends support arbitrary metadata; local filesystem may not.
	Metadata map[string]string

	// ETag is the entity tag for caching and conditional requests.
	// Used by cloud storage backends (S3, GCS, Azure).
	ETag string

	// Version is the version ID for versioned storage backends.
	// Empty for backends without versioning.
	Version string

	// StorageClass is the storage tier (e.g., "STANDARD", "GLACIER").
	// Backend-specific; may be empty.
	StorageClass string

	// Checksum is the pre-computed checksum if available from the backend.
	Checksum string

	// ChecksumAlgorithm indicates which algorithm was used for Checksum.
	ChecksumAlgorithm ChecksumAlgorithm

	// CreatedAt is the creation time (may not be available on all backends).
	CreatedAt *time.Time

	// AccessedAt is the last access time (may not be available on all backends).
	AccessedAt *time.Time

	// Owner contains file ownership information (optional, backend-specific).
	Owner *FileOwner

	// Permissions contains file permissions/ACL (optional, backend-specific).
	Permissions *FilePermissions
}

// FileOwner represents file ownership information.
type FileOwner struct {
	// ID is the user/account ID.
	ID string

	// DisplayName is a human-readable name.
	DisplayName string

	// Email is the email address (if available).
	Email string
}

// FilePermissions represents file permissions/ACL.
type FilePermissions struct {
	// Mode is Unix-style permissions (e.g., "0644").
	Mode string

	// ACL is the access control list.
	ACL []ACLEntry

	// IsPublic is a quick check for public access.
	IsPublic bool
}

// ACLEntry represents a single ACL entry.
type ACLEntry struct {
	// Grantee is the user/group ID.
	Grantee string

	// Permission is the permission type (READ, WRITE, FULL_CONTROL, etc.).
	Permission string

	// GranteeType is USER, GROUP, ALL_USERS, etc.
	GranteeType string
}

// WriteResult contains metadata about a completed write operation.
type WriteResult struct {
	// BytesWritten is the total number of bytes written.
	BytesWritten int64

	// Checksum is the computed checksum (if available).
	// Format depends on ChecksumAlgorithm used.
	Checksum string

	// ChecksumAlgorithm indicates which algorithm was used for Checksum.
	ChecksumAlgorithm ChecksumAlgorithm

	// Version is the version identifier (for versioned storage backends).
	// Empty for backends without versioning.
	Version string

	// ETag is the entity tag (S3, GCS, Azure).
	// Can be used for conditional requests and caching.
	ETag string

	// ServerTimestamp is when the server completed the write.
	ServerTimestamp time.Time

	// Metadata contains any additional backend-specific metadata.
	Metadata map[string]string
}

// ============================================================================
// Core Interfaces (Interface Segregation)
// ============================================================================

// FileReader provides read-only filesystem access.
// Use this type in function signatures to enforce read-only at compile time.
type FileReader interface {
	// Read returns a stream for reading file content.
	Read(ctx context.Context, path string) (io.ReadCloser, error)

	// ReadAll reads entire file into memory. Use for small files only.
	ReadAll(ctx context.Context, path string) ([]byte, error)

	// FileExists checks if a file exists at path.
	FileExists(ctx context.Context, path string) (bool, error)

	// DirExists checks if a directory exists at path.
	DirExists(ctx context.Context, path string) (bool, error)

	// Stat returns file/directory metadata.
	Stat(ctx context.Context, path string) (*FileInfo, error)

	// ListContents lists directory contents.
	// If recursive is true, includes all descendants.
	ListContents(ctx context.Context, path string, recursive bool) ([]FileInfo, error)
}

// FileWriter provides write filesystem operations.
type FileWriter interface {
	// Write writes content from reader to path.
	// Returns metadata about the write operation.
	// Use bytes.NewReader(data) for []byte, os.Open() for local files.
	Write(ctx context.Context, path string, r io.Reader, opts ...Option) (*WriteResult, error)

	// Delete removes a file.
	Delete(ctx context.Context, path string) error

	// CreateDir creates a directory (and parents if needed).
	CreateDir(ctx context.Context, path string) error

	// DeleteDir removes a directory and all contents.
	DeleteDir(ctx context.Context, path string) error
}

// FileSystem provides full read-write filesystem access.
type FileSystem interface {
	FileReader
	FileWriter
}

// ============================================================================
// Helper Functions (Aliases)
// ============================================================================

// GetFileInfo is an alias for Stat that provides a more descriptive name.
// It returns file or directory metadata for the given path.
//
// Example:
//
//	info, err := filekit.GetFileInfo(ctx, fs, "path/to/file.txt")
//	if err != nil {
//	    return err
//	}
//	fmt.Printf("Size: %d, Modified: %s\n", info.Size, info.ModTime)
func GetFileInfo(ctx context.Context, fs FileReader, path string) (*FileInfo, error) {
	return fs.Stat(ctx, path)
}

// ============================================================================
// Optional Capability Interfaces
// ============================================================================
// These interfaces allow drivers to expose optional capabilities.
// Use type assertion to check if a driver supports a capability:
//
//	if copier, ok := fs.(CanCopy); ok {
//	    copier.Copy(ctx, src, dst)
//	}

// CanCopy indicates the filesystem supports native copy operations.
// Native copy is more efficient than read+write for same-backend operations.
type CanCopy interface {
	Copy(ctx context.Context, src, dst string) error
}

// CanMove indicates the filesystem supports native move/rename operations.
// Native move is more efficient than copy+delete for same-backend operations.
type CanMove interface {
	Move(ctx context.Context, src, dst string) error
}

// ============================================================================
// Checksum Interface (FileKit Security Feature)
// ============================================================================

// ChecksumAlgorithm represents a supported checksum algorithm
type ChecksumAlgorithm string

const (
	// ChecksumMD5 is the MD5 hash algorithm (128-bit, fast but not cryptographically secure)
	ChecksumMD5 ChecksumAlgorithm = "md5"
	// ChecksumSHA1 is the SHA-1 hash algorithm (160-bit, legacy)
	ChecksumSHA1 ChecksumAlgorithm = "sha1"
	// ChecksumSHA256 is the SHA-256 hash algorithm (256-bit, recommended)
	ChecksumSHA256 ChecksumAlgorithm = "sha256"
	// ChecksumSHA512 is the SHA-512 hash algorithm (512-bit, most secure)
	ChecksumSHA512 ChecksumAlgorithm = "sha512"
	// ChecksumCRC32 is the CRC32 checksum (32-bit, fastest, for integrity only)
	ChecksumCRC32 ChecksumAlgorithm = "crc32"
	// ChecksumCRC32C is the CRC32C (Castagnoli) checksum used by cloud providers (S3, GCS)
	ChecksumCRC32C ChecksumAlgorithm = "crc32c"
	// ChecksumXXHash is the xxHash algorithm (64-bit, extremely fast)
	ChecksumXXHash ChecksumAlgorithm = "xxhash"
)

// CanChecksum indicates the filesystem supports integrity verification.
// This is a FileKit security feature for:
// - File integrity verification
// - Deduplication
// - Change detection
// - Security validation
//
// Example:
//
//	if cs, ok := fs.(CanChecksum); ok {
//	    hash, err := cs.Checksum(ctx, "file.txt", ChecksumSHA256)
//	    fmt.Printf("SHA256: %s\n", hash)
//	}
type CanChecksum interface {
	// Checksum calculates the checksum of a file using the specified algorithm.
	// Returns the checksum as a hex-encoded string.
	Checksum(ctx context.Context, path string, algorithm ChecksumAlgorithm) (string, error)

	// Checksums calculates multiple checksums in a single read pass.
	// Returns a map of algorithm to hex-encoded checksum.
	Checksums(ctx context.Context, path string, algorithms []ChecksumAlgorithm) (map[ChecksumAlgorithm]string, error)
}

// ============================================================================
// URL Generation Interfaces
// ============================================================================

// CanSignURL indicates the filesystem can generate pre-signed URLs.
// Useful for S3, GCS, Azure - allows direct client access without proxying.
type CanSignURL interface {
	// SignedURL creates a pre-signed URL for downloading a file.
	SignedURL(ctx context.Context, path string, expires time.Duration) (string, error)

	// SignedUploadURL creates a pre-signed URL for uploading a file.
	SignedUploadURL(ctx context.Context, path string, expires time.Duration) (string, error)
}

// ============================================================================
// File Watching Interface (ChangeToken Pattern)
// ============================================================================
// This follows Microsoft's IChangeToken pattern from ASP.NET Core.
// Benefits:
// - Simple interface (one method)
// - Works for all backends (native events OR polling)
// - Consumer decides how to react (poll HasChanged OR register callback)
// - Composable (combine multiple tokens)
// - Integrates with caching

// ChangeToken represents a change notification token.
// It provides a mechanism to be notified when a change occurs.
//
// Consumers can either:
// 1. Poll HasChanged() periodically
// 2. Register a callback via RegisterChangeCallback()
//
// Check ActiveChangeCallbacks() to know which approach is more efficient
// for the underlying implementation.
type ChangeToken interface {
	// HasChanged returns true if a change has occurred.
	// Once true, it remains true (tokens are single-use).
	HasChanged() bool

	// ActiveChangeCallbacks indicates if the token proactively raises callbacks.
	// If true, RegisterChangeCallback is efficient.
	// If false, consumers should poll HasChanged instead.
	ActiveChangeCallbacks() bool

	// RegisterChangeCallback registers a callback to be invoked when change occurs.
	// Returns a function to unregister the callback.
	// The callback receives no arguments - check the source for what changed.
	RegisterChangeCallback(callback func()) (unregister func())
}

// CanWatch indicates the filesystem supports file change notifications.
// Not all backends support watching - check with type assertion.
//
// Example:
//
//	if watcher, ok := fs.(CanWatch); ok {
//	    token, err := watcher.Watch(ctx, "**/*.json")
//	    if err != nil {
//	        log.Fatal(err)
//	    }
//
//	    // Option 1: Poll
//	    if token.HasChanged() {
//	        reloadConfig()
//	    }
//
//	    // Option 2: Callback (if supported)
//	    if token.ActiveChangeCallbacks() {
//	        unregister := token.RegisterChangeCallback(func() {
//	            log.Println("Change detected!")
//	            reloadConfig()
//	        })
//	        defer unregister()
//	    }
//	}
type CanWatch interface {
	// Watch creates a change token for the specified filter pattern.
	// Supports glob patterns: "**/*.txt", "config/*", "*.json", etc.
	// The token signals when any matching file is created, modified, or deleted.
	Watch(ctx context.Context, pattern string) (ChangeToken, error)
}

// ============================================================================
// Range Read Interface
// ============================================================================

// CanReadRange indicates the filesystem supports range reads.
// This is essential for:
// - Video streaming (byte-range requests)
// - Resume downloads
// - Reading file tails (logs)
// - Efficient partial file access
//
// Example:
//
//	if rangeReader, ok := fs.(CanReadRange); ok {
//	    // Read last 1KB of log file
//	    reader, err := rangeReader.ReadRange(ctx, "app.log", -1024, 1024)
//	}
type CanReadRange interface {
	// ReadRange reads a specific byte range from a file.
	//
	// offset: Starting position
	//   - If >= 0: absolute position from start
	//   - If < 0: position from end (e.g., -100 = last 100 bytes)
	//
	// length: Number of bytes to read
	//   - If > 0: read exactly this many bytes
	//   - If 0: read to end of file
	//   - If < 0: invalid (returns error)
	//
	// Returns io.ReadCloser positioned at offset.
	// Caller must close the reader.
	ReadRange(ctx context.Context, path string, offset, length int64) (io.ReadCloser, error)
}
