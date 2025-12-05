# Concrete API Changes - Code Examples

## How to implement the required changes

---

## 1. Write Return Value Change

### Step 1: Define WriteResult

**Add to `fs.go`:**

```go
// WriteResult contains metadata about a completed write operation
type WriteResult struct {
    // BytesWritten is the total number of bytes written
    BytesWritten int64

    // Checksum is the computed checksum (if available)
    // Format depends on ChecksumAlgorithm used
    Checksum string

    // ChecksumAlgorithm indicates which algorithm was used for Checksum
    ChecksumAlgorithm ChecksumAlgorithm

    // Version is the version identifier (for versioned storage backends)
    // Empty for backends without versioning
    Version string

    // ETag is the entity tag (S3, GCS, Azure)
    // Can be used for conditional requests and caching
    ETag string

    // ServerTimestamp is when the server completed the write
    ServerTimestamp time.Time

    // Metadata contains any additional backend-specific metadata
    Metadata map[string]string
}
```

### Step 2: Update FileWriter Interface

**Change in `fs.go`:**

```go
// FileWriter provides write filesystem operations.
type FileWriter interface {
    // Write writes content from reader to path.
    // Returns metadata about the write operation.
    Write(ctx context.Context, path string, r io.Reader, opts ...Option) (*WriteResult, error)

    // Delete removes a file.
    Delete(ctx context.Context, path string) error

    // CreateDir creates a directory (and parents if needed).
    CreateDir(ctx context.Context, path string) error

    // DeleteDir removes a directory and all contents.
    DeleteDir(ctx context.Context, path string) error
}
```

### Step 3: Update All Implementations

**Example - Local Driver:**

```go
func (l *localDriver) Write(ctx context.Context, path string, r io.Reader, opts ...Option) (*WriteResult, error) {
    // ... existing write logic

    // Calculate checksum while writing
    hash := sha256.New()
    written, err := io.Copy(io.MultiWriter(file, hash), r)
    if err != nil {
        return nil, err
    }

    // Get file info for timestamp
    stat, err := file.Stat()
    if err != nil {
        return nil, err
    }

    return &WriteResult{
        BytesWritten:      written,
        Checksum:          hex.EncodeToString(hash.Sum(nil)),
        ChecksumAlgorithm: ChecksumSHA256,
        ServerTimestamp:   stat.ModTime(),
        // Local filesystem doesn't have ETag or Version
    }, nil
}
```

**Example - S3 Driver:**

```go
func (s *s3Driver) Write(ctx context.Context, path string, r io.Reader, opts ...Option) (*WriteResult, error) {
    // ... existing S3 upload logic

    result, err := s.client.PutObject(ctx, &s3.PutObjectInput{
        Bucket: aws.String(s.bucket),
        Key:    aws.String(path),
        Body:   r,
    })
    if err != nil {
        return nil, err
    }

    return &WriteResult{
        BytesWritten:    *result.ContentLength,
        ETag:            aws.ToString(result.ETag),
        Version:         aws.ToString(result.VersionId),
        Checksum:        aws.ToString(result.ChecksumSHA256),
        ChecksumAlgorithm: ChecksumSHA256,
        ServerTimestamp: time.Now(),
    }, nil
}
```

### Step 4: Update All Decorators

**Example - EncryptedFS:**

```go
func (e *EncryptedFS) Write(ctx context.Context, path string, content io.Reader, options ...Option) (*WriteResult, error) {
    // ... encryption logic

    result, err := e.fs.Write(ctx, path, pr, options...)
    if err != nil {
        return nil, err
    }

    // Return result from underlying filesystem
    // Add encryption metadata if desired
    if result.Metadata == nil {
        result.Metadata = make(map[string]string)
    }
    result.Metadata["encrypted"] = "true"
    result.Metadata["encryption_algorithm"] = "AES-256-GCM"

    return result, nil
}
```

---

## 2. PathError Enhancement

### Step 1: Define Error Codes

**Add to `errors.go`:**

```go
// ErrorCode represents a structured error code for programmatic handling
type ErrorCode string

const (
    // File/Directory Errors
    ErrCodeNotFound         ErrorCode = "NOT_FOUND"
    ErrCodeAlreadyExists    ErrorCode = "ALREADY_EXISTS"
    ErrCodeNotEmpty         ErrorCode = "NOT_EMPTY"
    ErrCodeIsDirectory      ErrorCode = "IS_DIRECTORY"
    ErrCodeNotDirectory     ErrorCode = "NOT_DIRECTORY"

    // Permission Errors
    ErrCodePermissionDenied ErrorCode = "PERMISSION_DENIED"
    ErrCodeForbidden        ErrorCode = "FORBIDDEN"
    ErrCodeUnauthorized     ErrorCode = "UNAUTHORIZED"

    // Resource Errors
    ErrCodeQuotaExceeded    ErrorCode = "QUOTA_EXCEEDED"
    ErrCodeNoSpace          ErrorCode = "NO_SPACE"
    ErrCodeTooLarge         ErrorCode = "TOO_LARGE"

    // Validation Errors
    ErrCodeInvalidArgument  ErrorCode = "INVALID_ARGUMENT"
    ErrCodeInvalidPath      ErrorCode = "INVALID_PATH"
    ErrCodeInvalidName      ErrorCode = "INVALID_NAME"

    // State Errors
    ErrCodeClosed           ErrorCode = "CLOSED"
    ErrCodeCancelled        ErrorCode = "CANCELLED"
    ErrCodeDeadlineExceeded ErrorCode = "DEADLINE_EXCEEDED"

    // Network/Service Errors
    ErrCodeUnavailable      ErrorCode = "UNAVAILABLE"
    ErrCodeTimeout          ErrorCode = "TIMEOUT"
    ErrCodeNetworkError     ErrorCode = "NETWORK_ERROR"

    // Data Errors
    ErrCodeDataCorrupted    ErrorCode = "DATA_CORRUPTED"
    ErrCodeChecksumMismatch ErrorCode = "CHECKSUM_MISMATCH"
    ErrCodeVersionConflict  ErrorCode = "VERSION_CONFLICT"

    // Feature Errors
    ErrCodeNotSupported     ErrorCode = "NOT_SUPPORTED"
    ErrCodeNotImplemented   ErrorCode = "NOT_IMPLEMENTED"

    // Unknown
    ErrCodeInternal         ErrorCode = "INTERNAL"
    ErrCodeUnknown          ErrorCode = "UNKNOWN"
)
```

### Step 2: Enhance PathError

**Update in `errors.go`:**

```go
// PathError records an error and the operation and file path that caused it
type PathError struct {
    Op   string
    Path string
    Err  error

    // Code is a structured error code for programmatic handling
    Code ErrorCode

    // Retryable indicates if the operation can be retried
    Retryable bool

    // HTTPCode suggests an appropriate HTTP status code
    // 0 means no suggestion
    HTTPCode int

    // Context contains additional error context
    Context map[string]interface{}

    // Timestamp is when the error occurred
    Timestamp time.Time
}

// Error implements the error interface
func (e *PathError) Error() string {
    if e.Code != "" {
        return fmt.Sprintf("%s %s: [%s] %v", e.Op, e.Path, e.Code, e.Err)
    }
    return fmt.Sprintf("%s %s: %v", e.Op, e.Path, e.Err)
}

// Unwrap returns the underlying error
func (e *PathError) Unwrap() error {
    return e.Err
}

// IsCode checks if the error has the given error code
func (e *PathError) IsCode(code ErrorCode) bool {
    return e.Code == code
}

// IsRetryable returns true if the error is retryable
func (e *PathError) IsRetryable() bool {
    return e.Retryable
}

// WithContext adds context to the error
func (e *PathError) WithContext(key string, value interface{}) *PathError {
    if e.Context == nil {
        e.Context = make(map[string]interface{})
    }
    e.Context[key] = value
    return e
}
```

### Step 3: Add Helper Functions

**Add to `errors.go`:**

```go
// NewPathError creates a new PathError with sensible defaults
func NewPathError(op, path string, err error) *PathError {
    return &PathError{
        Op:        op,
        Path:      path,
        Err:       err,
        Code:      inferErrorCode(err),
        Retryable: isRetryableError(err),
        HTTPCode:  inferHTTPCode(err),
        Timestamp: time.Now(),
    }
}

// inferErrorCode tries to determine the error code from the error
func inferErrorCode(err error) ErrorCode {
    if err == nil {
        return ErrCodeUnknown
    }

    switch {
    case errors.Is(err, ErrNotExist):
        return ErrCodeNotFound
    case errors.Is(err, ErrExist):
        return ErrCodeAlreadyExists
    case errors.Is(err, ErrPermission):
        return ErrCodePermissionDenied
    case errors.Is(err, ErrNotSupported):
        return ErrCodeNotSupported
    case errors.Is(err, ErrIsDir):
        return ErrCodeIsDirectory
    case errors.Is(err, ErrNotDir):
        return ErrCodeNotDirectory
    case errors.Is(err, ErrNotEmpty):
        return ErrCodeNotEmpty
    case errors.Is(err, ErrNoSpace):
        return ErrCodeNoSpace
    case errors.Is(err, ErrInvalidSize):
        return ErrCodeTooLarge
    case errors.Is(err, context.Canceled):
        return ErrCodeCancelled
    case errors.Is(err, context.DeadlineExceeded):
        return ErrCodeDeadlineExceeded
    default:
        return ErrCodeUnknown
    }
}

// isRetryableError determines if an error is retryable
func isRetryableError(err error) bool {
    if err == nil {
        return false
    }

    // Check for context errors (not retryable)
    if errors.Is(err, context.Canceled) {
        return false
    }

    // Timeout is retryable
    if errors.Is(err, context.DeadlineExceeded) {
        return true
    }

    // Network errors are typically retryable
    // This would need more sophisticated logic in production

    return false
}

// inferHTTPCode suggests an HTTP status code
func inferHTTPCode(err error) int {
    if err == nil {
        return 0
    }

    switch {
    case errors.Is(err, ErrNotExist):
        return 404
    case errors.Is(err, ErrExist):
        return 409
    case errors.Is(err, ErrPermission):
        return 403
    case errors.Is(err, ErrNotSupported):
        return 501
    case errors.Is(err, ErrInvalidSize):
        return 413
    case errors.Is(err, context.Canceled):
        return 499
    case errors.Is(err, context.DeadlineExceeded):
        return 504
    default:
        return 500
    }
}
```

### Step 4: Update Error Checking Functions

**Add to `errors.go`:**

```go
// IsNotExist reports whether an error indicates that a file or directory
// does not exist
func IsNotExist(err error) bool {
    var pathErr *PathError
    if errors.As(err, &pathErr) {
        return pathErr.Code == ErrCodeNotFound
    }
    return errors.Is(err, ErrNotExist)
}

// IsExist reports whether an error indicates that a file or directory
// already exists
func IsExist(err error) bool {
    var pathErr *PathError
    if errors.As(err, &pathErr) {
        return pathErr.Code == ErrCodeAlreadyExists
    }
    return errors.Is(err, ErrExist)
}

// IsPermission reports whether an error indicates that permission is denied
func IsPermission(err error) bool {
    var pathErr *PathError
    if errors.As(err, &pathErr) {
        return pathErr.Code == ErrCodePermissionDenied || pathErr.Code == ErrCodeForbidden
    }
    return errors.Is(err, ErrPermission)
}

// IsRetryable reports whether an error is retryable
func IsRetryable(err error) bool {
    var pathErr *PathError
    if errors.As(err, &pathErr) {
        return pathErr.Retryable
    }
    return false
}

// GetErrorCode extracts the error code from an error
func GetErrorCode(err error) ErrorCode {
    var pathErr *PathError
    if errors.As(err, &pathErr) {
        return pathErr.Code
    }
    return ErrCodeUnknown
}
```

---

## 3. FileInfo Enhancement

### Step 1: Update FileInfo Struct

**Update in `fs.go`:**

```go
// FileInfo represents file or directory metadata
type FileInfo struct {
    // Basic fields (existing)
    Name        string
    Path        string
    Size        int64
    ModTime     time.Time
    IsDir       bool
    ContentType string
    Metadata    map[string]string

    // Caching and versioning (NEW)
    ETag         string     // Entity tag for caching and conditional requests
    Version      string     // Version ID for versioned storage
    StorageClass string     // Storage tier (e.g., "STANDARD", "GLACIER")
    Checksum     string     // Pre-computed checksum if available
    ChecksumAlgorithm ChecksumAlgorithm // Algorithm used for Checksum

    // Timestamps (NEW)
    CreatedAt    *time.Time // Creation time (may not be available on all backends)
    AccessedAt   *time.Time // Last access time (may not be available)

    // Access control (NEW - optional, backend-specific)
    Owner       *FileOwner       // Owner information
    Permissions *FilePermissions // Permissions/ACL
}

// FileOwner represents file ownership information
type FileOwner struct {
    ID          string // User/account ID
    DisplayName string // Human-readable name
    Email       string // Email address (if available)
}

// FilePermissions represents file permissions/ACL
type FilePermissions struct {
    Mode     string   // Unix-style permissions (e.g., "0644")
    ACL      []ACLEntry // Access control list
    IsPublic bool     // Quick check for public access
}

// ACLEntry represents a single ACL entry
type ACLEntry struct {
    Grantee     string // User/group ID
    Permission  string // Permission type (READ, WRITE, FULL_CONTROL, etc.)
    GranteeType string // USER, GROUP, ALL_USERS, etc.
}
```

---

## 4. Add CanReadRange Capability

### Step 1: Define Interface

**Add to `fs.go`:**

```go
// CanReadRange indicates the filesystem supports range reads.
// This is essential for:
// - Video streaming (byte-range requests)
// - Resume downloads
// - Reading file tails (logs)
// - Efficient partial file access
//
// Example:
//
//  if rangeReader, ok := fs.(CanReadRange); ok {
//      // Read last 1KB of log file
//      reader, err := rangeReader.ReadRange(ctx, "app.log", -1024, 1024)
//  }
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
```

### Step 2: Implement in Drivers

**Example - S3:**

```go
func (s *s3Driver) ReadRange(ctx context.Context, path string, offset, length int64) (io.ReadCloser, error) {
    // Build range string
    var rangeStr string
    if offset >= 0 {
        if length > 0 {
            rangeStr = fmt.Sprintf("bytes=%d-%d", offset, offset+length-1)
        } else {
            rangeStr = fmt.Sprintf("bytes=%d-", offset)
        }
    } else {
        // Negative offset = from end
        if length > 0 {
            rangeStr = fmt.Sprintf("bytes=%d", offset)
        } else {
            return nil, fmt.Errorf("invalid range: negative offset with zero length")
        }
    }

    result, err := s.client.GetObject(ctx, &s3.GetObjectInput{
        Bucket: aws.String(s.bucket),
        Key:    aws.String(path),
        Range:  aws.String(rangeStr),
    })
    if err != nil {
        return nil, NewPathError("read_range", path, err)
    }

    return result.Body, nil
}
```

**Example - Local:**

```go
func (l *localDriver) ReadRange(ctx context.Context, path string, offset, length int64) (io.ReadCloser, error) {
    fullPath := filepath.Join(l.basePath, path)

    file, err := os.Open(fullPath)
    if err != nil {
        return nil, NewPathError("read_range", path, err)
    }

    // Handle negative offset (from end)
    if offset < 0 {
        stat, err := file.Stat()
        if err != nil {
            file.Close()
            return nil, err
        }
        offset = stat.Size() + offset
        if offset < 0 {
            offset = 0
        }
    }

    // Seek to position
    _, err = file.Seek(offset, io.SeekStart)
    if err != nil {
        file.Close()
        return nil, err
    }

    // If length specified, wrap in LimitReader
    if length > 0 {
        return &limitedReadCloser{
            ReadCloser: file,
            Reader:     io.LimitReader(file, length),
        }, nil
    }

    return file, nil
}

type limitedReadCloser struct {
    io.ReadCloser
    io.Reader
}

func (l *limitedReadCloser) Read(p []byte) (n int, err error) {
    return l.Reader.Read(p)
}
```

---

## 5. Migration Examples

### For Library Users

**Before:**
```go
// Old code
err := fs.Write(ctx, "file.txt", reader)
if err != nil {
    log.Printf("Write failed: %v", err)
    return err
}
log.Println("File written successfully")
```

**After:**
```go
// New code
result, err := fs.Write(ctx, "file.txt", reader)
if err != nil {
    var pathErr *filekit.PathError
    if errors.As(err, &pathErr) {
        log.Printf("Write failed: %v (code: %s, retryable: %v)",
            err, pathErr.Code, pathErr.Retryable)

        if pathErr.IsRetryable() {
            // Retry logic here
        }
    }
    return err
}

log.Printf("File written successfully: %d bytes, ETag: %s",
    result.BytesWritten, result.ETag)

// Use ETag for caching
cacheKey := fmt.Sprintf("file:%s:etag:%s", "file.txt", result.ETag)
```

### For Driver Implementers

**Before:**
```go
func (d *myDriver) Write(ctx context.Context, path string, r io.Reader, opts ...Option) error {
    // ... implementation
    return nil
}
```

**After:**
```go
func (d *myDriver) Write(ctx context.Context, path string, r io.Reader, opts ...Option) (*WriteResult, error) {
    start := time.Now()
    var written int64

    // ... implementation with byte counting

    return &WriteResult{
        BytesWritten:    written,
        ETag:            computeETag(path, written),
        ServerTimestamp: start,
    }, nil
}
```

---

## Testing the Changes

### Test Write Return Value

```go
func TestWriteReturnsMetadata(t *testing.T) {
    fs := setupTestFS(t)
    ctx := context.Background()

    content := []byte("test content")
    reader := bytes.NewReader(content)

    result, err := fs.Write(ctx, "test.txt", reader)
    require.NoError(t, err)
    require.NotNil(t, result)

    // Verify result
    assert.Equal(t, int64(len(content)), result.BytesWritten)
    assert.NotEmpty(t, result.ETag)
    assert.NotZero(t, result.ServerTimestamp)

    // If checksums are supported
    if result.Checksum != "" {
        expectedHash := sha256.Sum256(content)
        assert.Equal(t, hex.EncodeToString(expectedHash[:]), result.Checksum)
    }
}
```

### Test Error Codes

```go
func TestErrorCodes(t *testing.T) {
    fs := setupTestFS(t)
    ctx := context.Background()

    // Test not found
    _, err := fs.Read(ctx, "nonexistent.txt")
    require.Error(t, err)

    var pathErr *filekit.PathError
    require.True(t, errors.As(err, &pathErr))
    assert.Equal(t, filekit.ErrCodeNotFound, pathErr.Code)
    assert.Equal(t, 404, pathErr.HTTPCode)
    assert.False(t, pathErr.Retryable)

    // Helper function should work
    assert.True(t, filekit.IsNotExist(err))
}
```

### Test Range Reads

```go
func TestRangeRead(t *testing.T) {
    fs := setupTestFS(t)
    ctx := context.Background()

    // Write test file
    content := []byte("0123456789")
    _, err := fs.Write(ctx, "test.txt", bytes.NewReader(content))
    require.NoError(t, err)

    // Test range read capability
    rangeReader, ok := fs.(filekit.CanReadRange)
    require.True(t, ok, "filesystem doesn't support range reads")

    // Read middle bytes
    reader, err := rangeReader.ReadRange(ctx, "test.txt", 3, 4)
    require.NoError(t, err)
    defer reader.Close()

    data, err := io.ReadAll(reader)
    require.NoError(t, err)
    assert.Equal(t, "3456", string(data))

    // Read from end
    reader, err = rangeReader.ReadRange(ctx, "test.txt", -3, 3)
    require.NoError(t, err)
    defer reader.Close()

    data, err = io.ReadAll(reader)
    require.NoError(t, err)
    assert.Equal(t, "789", string(data))
}
```

---

## Summary

These concrete examples show exactly how to implement the required API changes:

1. ✅ **WriteResult**: New struct + interface change + update all implementations
2. ✅ **PathError**: Add ErrorCode + helper functions + update error creation
3. ✅ **FileInfo**: Add new fields (backwards compatible)
4. ✅ **CanReadRange**: New capability interface + implementations

Each change includes:
- Exact code to add
- Where to add it
- How to implement in drivers
- How to test it
- Migration examples

Start with these changes, test thoroughly, then release as v1.0.
