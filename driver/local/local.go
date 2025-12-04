package local

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/gobeaver/filekit"
)

// Adapter provides a local filesystem implementation of filekit.FileSystem
type Adapter struct {
	root string
}

// New creates a new local filesystem adapter
func New(root string) (*Adapter, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}

	// Ensure the root directory exists
	if err := os.MkdirAll(absRoot, 0755); err != nil {
		return nil, err
	}

	return &Adapter{
		root: absRoot,
	}, nil
}

// Write implements filekit.FileWriter
func (a *Adapter) Write(ctx context.Context, path string, content io.Reader, options ...filekit.Option) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		// Continue
	}

	fullPath := filepath.Join(a.root, filepath.Clean(path))

	// Check if the path is under the root
	if !isPathUnderRoot(a.root, fullPath) {
		return &filekit.PathError{
			Op:   "write",
			Path: path,
			Err:  filekit.ErrNotAllowed,
		}
	}

	// Ensure the directory exists
	dir := filepath.Dir(fullPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return &filekit.PathError{
			Op:   "write",
			Path: path,
			Err:  err,
		}
	}

	// Create the file
	f, err := os.Create(fullPath)
	if err != nil {
		return &filekit.PathError{
			Op:   "write",
			Path: path,
			Err:  err,
		}
	}
	defer f.Close()

	// Copy the content to the file
	_, err = io.Copy(f, content)
	if err != nil {
		return &filekit.PathError{
			Op:   "write",
			Path: path,
			Err:  err,
		}
	}

	// Apply file options (permissions, etc.) if needed
	opts := processOptions(options...)

	// Set file permissions based on visibility
	if opts.Visibility == filekit.Public {
		if err := os.Chmod(fullPath, 0644); err != nil {
			return &filekit.PathError{
				Op:   "write",
				Path: path,
				Err:  err,
			}
		}
	} else if opts.Visibility == filekit.Private {
		if err := os.Chmod(fullPath, 0600); err != nil {
			return &filekit.PathError{
				Op:   "write",
				Path: path,
				Err:  err,
			}
		}
	}

	return nil
}

// Read implements filekit.FileReader
func (a *Adapter) Read(ctx context.Context, path string) (io.ReadCloser, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
		// Continue
	}

	fullPath := filepath.Join(a.root, filepath.Clean(path))

	// Check if the path is under the root
	if !isPathUnderRoot(a.root, fullPath) {
		return nil, &filekit.PathError{
			Op:   "read",
			Path: path,
			Err:  filekit.ErrNotAllowed,
		}
	}

	// Open the file
	f, err := os.Open(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, &filekit.PathError{
				Op:   "read",
				Path: path,
				Err:  filekit.ErrNotExist,
			}
		}
		return nil, &filekit.PathError{
			Op:   "read",
			Path: path,
			Err:  err,
		}
	}

	return f, nil
}

// ReadAll implements filekit.FileReader
func (a *Adapter) ReadAll(ctx context.Context, path string) ([]byte, error) {
	rc, err := a.Read(ctx, path)
	if err != nil {
		return nil, err
	}
	defer rc.Close()

	return io.ReadAll(rc)
}

// Delete implements filekit.FileSystem
func (a *Adapter) Delete(ctx context.Context, path string) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		// Continue
	}

	fullPath := filepath.Join(a.root, filepath.Clean(path))

	// Check if the path is under the root
	if !isPathUnderRoot(a.root, fullPath) {
		return &filekit.PathError{
			Op:   "delete",
			Path: path,
			Err:  filekit.ErrNotAllowed,
		}
	}

	// Delete the file
	err := os.Remove(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			return &filekit.PathError{
				Op:   "delete",
				Path: path,
				Err:  filekit.ErrNotExist,
			}
		}
		return &filekit.PathError{
			Op:   "delete",
			Path: path,
			Err:  err,
		}
	}

	return nil
}

// FileExists implements filekit.FileReader
func (a *Adapter) FileExists(ctx context.Context, path string) (bool, error) {
	select {
	case <-ctx.Done():
		return false, ctx.Err()
	default:
		// Continue
	}

	fullPath := filepath.Join(a.root, filepath.Clean(path))

	// Check if the path is under the root
	if !isPathUnderRoot(a.root, fullPath) {
		return false, &filekit.PathError{
			Op:   "fileexists",
			Path: path,
			Err:  filekit.ErrNotAllowed,
		}
	}

	info, err := os.Stat(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, &filekit.PathError{
			Op:   "fileexists",
			Path: path,
			Err:  err,
		}
	}

	// Return true only if it's a file (not a directory)
	return !info.IsDir(), nil
}

// DirExists implements filekit.FileReader
func (a *Adapter) DirExists(ctx context.Context, path string) (bool, error) {
	select {
	case <-ctx.Done():
		return false, ctx.Err()
	default:
		// Continue
	}

	fullPath := filepath.Join(a.root, filepath.Clean(path))

	// Check if the path is under the root
	if !isPathUnderRoot(a.root, fullPath) {
		return false, &filekit.PathError{
			Op:   "direxists",
			Path: path,
			Err:  filekit.ErrNotAllowed,
		}
	}

	info, err := os.Stat(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, &filekit.PathError{
			Op:   "direxists",
			Path: path,
			Err:  err,
		}
	}

	// Return true only if it's a directory
	return info.IsDir(), nil
}

// Stat implements filekit.FileReader
func (a *Adapter) Stat(ctx context.Context, path string) (*filekit.FileInfo, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
		// Continue
	}

	fullPath := filepath.Join(a.root, filepath.Clean(path))

	// Check if the path is under the root
	if !isPathUnderRoot(a.root, fullPath) {
		return nil, &filekit.PathError{
			Op:   "stat",
			Path: path,
			Err:  filekit.ErrNotAllowed,
		}
	}

	info, err := os.Stat(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, &filekit.PathError{
				Op:   "stat",
				Path: path,
				Err:  filekit.ErrNotExist,
			}
		}
		return nil, &filekit.PathError{
			Op:   "stat",
			Path: path,
			Err:  err,
		}
	}

	// Get content type
	contentType := ""
	if !info.IsDir() {
		contentType = getContentType(fullPath)
	}

	return &filekit.FileInfo{
		Name:        filepath.Base(path),
		Path:        path,
		Size:        info.Size(),
		ModTime:     info.ModTime(),
		IsDir:       info.IsDir(),
		ContentType: contentType,
	}, nil
}

// ListContents implements filekit.FileReader
func (a *Adapter) ListContents(ctx context.Context, path string, recursive bool) ([]filekit.FileInfo, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
		// Continue
	}

	fullPath := filepath.Join(a.root, filepath.Clean(path))

	// Check if the path is under the root
	if !isPathUnderRoot(a.root, fullPath) {
		return nil, &filekit.PathError{
			Op:   "listcontents",
			Path: path,
			Err:  filekit.ErrNotAllowed,
		}
	}

	// Check if the directory exists
	info, err := os.Stat(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, &filekit.PathError{
				Op:   "listcontents",
				Path: path,
				Err:  filekit.ErrNotExist,
			}
		}
		return nil, &filekit.PathError{
			Op:   "listcontents",
			Path: path,
			Err:  err,
		}
	}

	// If it's not a directory, return an error
	if !info.IsDir() {
		return nil, &filekit.PathError{
			Op:   "listcontents",
			Path: path,
			Err:  filekit.ErrNotDir,
		}
	}

	var files []filekit.FileInfo

	if recursive {
		// Walk the directory tree recursively
		err = filepath.Walk(fullPath, func(walkPath string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}

			// Skip the root directory itself
			if walkPath == fullPath {
				return nil
			}

			// Check context cancellation
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}

			relPath, err := filepath.Rel(a.root, walkPath)
			if err != nil {
				return err
			}

			contentType := ""
			if !info.IsDir() {
				contentType = getContentType(walkPath)
			}

			files = append(files, filekit.FileInfo{
				Name:        info.Name(),
				Path:        relPath,
				Size:        info.Size(),
				ModTime:     info.ModTime(),
				IsDir:       info.IsDir(),
				ContentType: contentType,
			})

			return nil
		})
		if err != nil {
			return nil, &filekit.PathError{
				Op:   "listcontents",
				Path: path,
				Err:  err,
			}
		}
	} else {
		// Read only the immediate directory contents
		entries, err := os.ReadDir(fullPath)
		if err != nil {
			return nil, &filekit.PathError{
				Op:   "listcontents",
				Path: path,
				Err:  err,
			}
		}

		files = make([]filekit.FileInfo, 0, len(entries))
		for _, entry := range entries {
			entryPath := filepath.Join(path, entry.Name())
			info, err := entry.Info()
			if err != nil {
				continue
			}

			contentType := ""
			if !info.IsDir() {
				contentType = getContentType(filepath.Join(a.root, entryPath))
			}

			files = append(files, filekit.FileInfo{
				Name:        entry.Name(),
				Path:        entryPath,
				Size:        info.Size(),
				ModTime:     info.ModTime(),
				IsDir:       info.IsDir(),
				ContentType: contentType,
			})
		}
	}

	return files, nil
}

// CreateDir implements filekit.FileSystem
func (a *Adapter) CreateDir(ctx context.Context, path string) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		// Continue
	}

	fullPath := filepath.Join(a.root, filepath.Clean(path))

	// Check if the path is under the root
	if !isPathUnderRoot(a.root, fullPath) {
		return &filekit.PathError{
			Op:   "createdir",
			Path: path,
			Err:  filekit.ErrNotAllowed,
		}
	}

	// Create the directory
	if err := os.MkdirAll(fullPath, 0755); err != nil {
		return &filekit.PathError{
			Op:   "createdir",
			Path: path,
			Err:  err,
		}
	}

	return nil
}

// DeleteDir implements filekit.FileSystem
func (a *Adapter) DeleteDir(ctx context.Context, path string) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		// Continue
	}

	fullPath := filepath.Join(a.root, filepath.Clean(path))

	// Check if the path is under the root
	if !isPathUnderRoot(a.root, fullPath) {
		return &filekit.PathError{
			Op:   "deletedir",
			Path: path,
			Err:  filekit.ErrNotAllowed,
		}
	}

	// Check if the directory exists
	info, err := os.Stat(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			return &filekit.PathError{
				Op:   "deletedir",
				Path: path,
				Err:  filekit.ErrNotExist,
			}
		}
		return &filekit.PathError{
			Op:   "deletedir",
			Path: path,
			Err:  err,
		}
	}

	// Check if it's a directory
	if !info.IsDir() {
		return &filekit.PathError{
			Op:   "deletedir",
			Path: path,
			Err:  filekit.ErrNotDir,
		}
	}

	// Delete the directory
	if err := os.RemoveAll(fullPath); err != nil {
		return &filekit.PathError{
			Op:   "deletedir",
			Path: path,
			Err:  err,
		}
	}

	return nil
}

// WriteFile writes a local file to the filesystem
func (a *Adapter) WriteFile(ctx context.Context, path string, localPath string, options ...filekit.Option) error {
	// Open the local file
	file, err := os.Open(localPath)
	if err != nil {
		return &filekit.PathError{
			Op:   "writefile",
			Path: localPath,
			Err:  err,
		}
	}
	defer file.Close()

	// Write the file
	return a.Write(ctx, path, file, options...)
}

// isPathUnderRoot checks if a path is under a given root directory
func isPathUnderRoot(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}

	return !filepath.IsAbs(rel) && !strings.HasPrefix(rel, "../")
}

// getContentType tries to determine the content type of a file
func getContentType(path string) string {
	// Try to determine content type from extension
	ext := filepath.Ext(path)
	if ext != "" {
		if contentType := mime.TypeByExtension(ext); contentType != "" {
			return contentType
		}
	}

	// Try to determine content type by reading file header
	file, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer file.Close()

	// Read a small slice of the file to detect content type
	buffer := make([]byte, 512)
	n, err := file.Read(buffer)
	if err != nil && !errors.Is(err, io.EOF) {
		return ""
	}

	return http.DetectContentType(buffer[:n])
}

// processOptions processes the provided options
func processOptions(options ...filekit.Option) *filekit.Options {
	opts := &filekit.Options{}
	for _, option := range options {
		option(opts)
	}
	return opts
}

// ============================================================================
// Optional Capability Interfaces
// ============================================================================

// Copy implements filekit.CanCopy for native file copying.
func (a *Adapter) Copy(ctx context.Context, src, dst string) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	srcPath := filepath.Join(a.root, filepath.Clean(src))
	dstPath := filepath.Join(a.root, filepath.Clean(dst))

	// Check paths are under root
	if !isPathUnderRoot(a.root, srcPath) {
		return &filekit.PathError{Op: "copy", Path: src, Err: filekit.ErrNotAllowed}
	}
	if !isPathUnderRoot(a.root, dstPath) {
		return &filekit.PathError{Op: "copy", Path: dst, Err: filekit.ErrNotAllowed}
	}

	// Open source file
	srcFile, err := os.Open(srcPath)
	if err != nil {
		if os.IsNotExist(err) {
			return &filekit.PathError{Op: "copy", Path: src, Err: filekit.ErrNotExist}
		}
		return &filekit.PathError{Op: "copy", Path: src, Err: err}
	}
	defer srcFile.Close()

	// Create destination directory if needed
	if err := os.MkdirAll(filepath.Dir(dstPath), 0755); err != nil {
		return &filekit.PathError{Op: "copy", Path: dst, Err: err}
	}

	// Create destination file
	dstFile, err := os.Create(dstPath)
	if err != nil {
		return &filekit.PathError{Op: "copy", Path: dst, Err: err}
	}
	defer dstFile.Close()

	// Copy content
	if _, err := io.Copy(dstFile, srcFile); err != nil {
		return &filekit.PathError{Op: "copy", Path: dst, Err: err}
	}

	// Copy file permissions
	srcInfo, err := os.Stat(srcPath)
	if err == nil {
		os.Chmod(dstPath, srcInfo.Mode())
	}

	return nil
}

// Move implements filekit.CanMove for native file moving/renaming.
func (a *Adapter) Move(ctx context.Context, src, dst string) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	srcPath := filepath.Join(a.root, filepath.Clean(src))
	dstPath := filepath.Join(a.root, filepath.Clean(dst))

	// Check paths are under root
	if !isPathUnderRoot(a.root, srcPath) {
		return &filekit.PathError{Op: "move", Path: src, Err: filekit.ErrNotAllowed}
	}
	if !isPathUnderRoot(a.root, dstPath) {
		return &filekit.PathError{Op: "move", Path: dst, Err: filekit.ErrNotAllowed}
	}

	// Check source exists
	if _, err := os.Stat(srcPath); err != nil {
		if os.IsNotExist(err) {
			return &filekit.PathError{Op: "move", Path: src, Err: filekit.ErrNotExist}
		}
		return &filekit.PathError{Op: "move", Path: src, Err: err}
	}

	// Create destination directory if needed
	if err := os.MkdirAll(filepath.Dir(dstPath), 0755); err != nil {
		return &filekit.PathError{Op: "move", Path: dst, Err: err}
	}

	// Try rename first (works if same filesystem)
	if err := os.Rename(srcPath, dstPath); err != nil {
		// If rename fails (cross-device), fall back to copy+delete
		if err := a.Copy(ctx, src, dst); err != nil {
			return err
		}
		if err := os.Remove(srcPath); err != nil {
			return &filekit.PathError{Op: "move", Path: src, Err: err}
		}
	}

	return nil
}


// Checksum implements filekit.CanChecksum for local files.
func (a *Adapter) Checksum(ctx context.Context, path string, algorithm filekit.ChecksumAlgorithm) (string, error) {
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	default:
	}

	fullPath := filepath.Join(a.root, filepath.Clean(path))

	if !isPathUnderRoot(a.root, fullPath) {
		return "", &filekit.PathError{Op: "checksum", Path: path, Err: filekit.ErrNotAllowed}
	}

	file, err := os.Open(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", &filekit.PathError{Op: "checksum", Path: path, Err: filekit.ErrNotExist}
		}
		return "", &filekit.PathError{Op: "checksum", Path: path, Err: err}
	}
	defer file.Close()

	checksum, err := filekit.CalculateChecksum(file, algorithm)
	if err != nil {
		return "", &filekit.PathError{Op: "checksum", Path: path, Err: err}
	}

	return checksum, nil
}

// Checksums implements filekit.MultiChecksummer for efficient multi-hash calculation.
func (a *Adapter) Checksums(ctx context.Context, path string, algorithms []filekit.ChecksumAlgorithm) (map[filekit.ChecksumAlgorithm]string, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	fullPath := filepath.Join(a.root, filepath.Clean(path))

	if !isPathUnderRoot(a.root, fullPath) {
		return nil, &filekit.PathError{Op: "checksums", Path: path, Err: filekit.ErrNotAllowed}
	}

	file, err := os.Open(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, &filekit.PathError{Op: "checksums", Path: path, Err: filekit.ErrNotExist}
		}
		return nil, &filekit.PathError{Op: "checksums", Path: path, Err: err}
	}
	defer file.Close()

	checksums, err := filekit.CalculateChecksums(file, algorithms)
	if err != nil {
		return nil, &filekit.PathError{Op: "checksums", Path: path, Err: err}
	}

	return checksums, nil
}

// Watch implements filekit.CanWatch using fsnotify for native file system events.
func (a *Adapter) Watch(ctx context.Context, filter string) (filekit.ChangeToken, error) {
	// Create a callback token that we'll signal when changes occur
	token := filekit.NewCallbackChangeToken()

	// Determine the directory to watch based on the filter
	watchPath := a.root
	filterPattern := filter

	// If filter starts with a path, extract it
	if !strings.HasPrefix(filter, "*") {
		// Find the first glob character
		idx := strings.IndexAny(filter, "*?[")
		if idx > 0 {
			// Get the directory part before the glob
			dirPart := filter[:idx]
			if lastSlash := strings.LastIndex(dirPart, "/"); lastSlash >= 0 {
				watchPath = filepath.Join(a.root, dirPart[:lastSlash])
				filterPattern = filter[lastSlash+1:]
			}
		} else if idx < 0 {
			// No glob - watch specific file
			watchPath = filepath.Join(a.root, filepath.Dir(filter))
			filterPattern = filepath.Base(filter)
		}
	}

	// Create fsnotify watcher
	watcher, err := newFSWatcher()
	if err != nil {
		return nil, &filekit.PathError{Op: "watch", Path: filter, Err: err}
	}

	// Add the watch path
	if err := watcher.Add(watchPath); err != nil {
		watcher.Close()
		return nil, &filekit.PathError{Op: "watch", Path: filter, Err: err}
	}

	// For recursive patterns (**), add all subdirectories
	if strings.Contains(filter, "**") {
		filepath.Walk(watchPath, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return nil
			}
			if info.IsDir() {
				watcher.Add(path)
			}
			return nil
		})
	}

	// Start goroutine to process events
	go func() {
		defer watcher.Close()

		for {
			select {
			case <-ctx.Done():
				return
			case event, ok := <-watcher.Events():
				if !ok {
					return
				}

				// Check if the event matches our filter pattern
				relPath, err := filepath.Rel(a.root, event.Name)
				if err != nil {
					continue
				}

				if matchesFilter(relPath, filter) || matchesFilter(filepath.Base(relPath), filterPattern) {
					token.SignalChange()
					return // Token is spent after first change
				}
			case _, ok := <-watcher.Errors():
				if !ok {
					return
				}
				// Log error but continue watching
			}
		}
	}()

	return token, nil
}

// matchesFilter checks if a path matches a glob filter pattern.
func matchesFilter(path, filter string) bool {
	// Handle ** recursive pattern
	if strings.Contains(filter, "**") {
		// Convert ** to match any path
		parts := strings.Split(filter, "**")
		if len(parts) == 2 {
			prefix := strings.TrimSuffix(parts[0], "/")
			suffix := strings.TrimPrefix(parts[1], "/")

			if prefix != "" && !strings.HasPrefix(path, prefix) {
				return false
			}
			if suffix != "" {
				matched, _ := filepath.Match(suffix, filepath.Base(path))
				return matched
			}
			return true
		}
	}

	// Standard glob match
	matched, _ := filepath.Match(filter, path)
	if matched {
		return true
	}

	// Try matching just the filename
	matched, _ = filepath.Match(filter, filepath.Base(path))
	return matched
}

// fsWatcher wraps fsnotify.Watcher with a simpler interface
type fsWatcher interface {
	Add(path string) error
	Close() error
	Events() <-chan fsEvent
	Errors() <-chan error
}

type fsEvent struct {
	Name string
	Op   uint32
}

// ============================================================================
// Chunked Upload Implementation
// ============================================================================

// uploadInfo stores metadata for an in-progress chunked upload.
type uploadInfo struct {
	path     string // Target path for the final file
	partsDir string // Directory storing uploaded parts
}

// uploadRegistry is a thread-safe registry for in-progress uploads.
var uploadRegistry = struct {
	sync.RWMutex
	uploads map[string]*uploadInfo
}{
	uploads: make(map[string]*uploadInfo),
}

// generateUploadID creates a unique upload identifier.
func generateUploadID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// InitiateUpload starts a chunked upload process and returns an upload ID.
// Parts are stored in a temporary directory until CompleteUpload is called.
func (a *Adapter) InitiateUpload(ctx context.Context, path string) (string, error) {
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	default:
	}

	fullPath := filepath.Join(a.root, filepath.Clean(path))

	// Check if the path is under the root
	if !isPathUnderRoot(a.root, fullPath) {
		return "", &filekit.PathError{
			Op:   "initiate-upload",
			Path: path,
			Err:  filekit.ErrNotAllowed,
		}
	}

	// Generate a unique upload ID
	uploadID, err := generateUploadID()
	if err != nil {
		return "", &filekit.PathError{
			Op:   "initiate-upload",
			Path: path,
			Err:  err,
		}
	}

	// Create a temporary directory for storing parts
	partsDir, err := os.MkdirTemp("", fmt.Sprintf("filekit-upload-%s-", uploadID))
	if err != nil {
		return "", &filekit.PathError{
			Op:   "initiate-upload",
			Path: path,
			Err:  err,
		}
	}

	// Store upload info
	uploadRegistry.Lock()
	uploadRegistry.uploads[uploadID] = &uploadInfo{
		path:     path,
		partsDir: partsDir,
	}
	uploadRegistry.Unlock()

	return uploadID, nil
}

// UploadPart uploads a part of a file in a chunked upload process.
// Parts are stored as numbered files (1, 2, 3, ...) in the temporary directory.
func (a *Adapter) UploadPart(ctx context.Context, uploadID string, partNumber int, data []byte) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	// Validate part number
	if partNumber < 1 {
		return &filekit.PathError{
			Op:   "upload-part",
			Path: uploadID,
			Err:  fmt.Errorf("part number must be >= 1, got %d", partNumber),
		}
	}

	// Get upload info
	uploadRegistry.RLock()
	info, ok := uploadRegistry.uploads[uploadID]
	uploadRegistry.RUnlock()

	if !ok {
		return &filekit.PathError{
			Op:   "upload-part",
			Path: uploadID,
			Err:  fmt.Errorf("upload not found: %s", uploadID),
		}
	}

	// Write part to file
	partPath := filepath.Join(info.partsDir, fmt.Sprintf("%d", partNumber))
	if err := os.WriteFile(partPath, data, 0600); err != nil {
		return &filekit.PathError{
			Op:   "upload-part",
			Path: uploadID,
			Err:  err,
		}
	}

	return nil
}

// CompleteUpload finalizes a chunked upload by concatenating all parts.
// Parts are read in numerical order and written to the target file.
func (a *Adapter) CompleteUpload(ctx context.Context, uploadID string) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	// Get and remove upload info
	uploadRegistry.Lock()
	info, ok := uploadRegistry.uploads[uploadID]
	if ok {
		delete(uploadRegistry.uploads, uploadID)
	}
	uploadRegistry.Unlock()

	if !ok {
		return &filekit.PathError{
			Op:   "complete-upload",
			Path: uploadID,
			Err:  fmt.Errorf("upload not found: %s", uploadID),
		}
	}

	// Ensure cleanup of parts directory
	defer os.RemoveAll(info.partsDir)

	// Read all part files
	entries, err := os.ReadDir(info.partsDir)
	if err != nil {
		return &filekit.PathError{
			Op:   "complete-upload",
			Path: uploadID,
			Err:  err,
		}
	}

	if len(entries) == 0 {
		return &filekit.PathError{
			Op:   "complete-upload",
			Path: uploadID,
			Err:  errors.New("no parts uploaded"),
		}
	}

	// Sort parts by part number
	partNumbers := make([]int, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		num, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}
		partNumbers = append(partNumbers, num)
	}
	sort.Ints(partNumbers)

	// Prepare target path
	fullPath := filepath.Join(a.root, filepath.Clean(info.path))

	// Ensure the directory exists
	dir := filepath.Dir(fullPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return &filekit.PathError{
			Op:   "complete-upload",
			Path: info.path,
			Err:  err,
		}
	}

	// Create the target file
	targetFile, err := os.Create(fullPath)
	if err != nil {
		return &filekit.PathError{
			Op:   "complete-upload",
			Path: info.path,
			Err:  err,
		}
	}
	defer targetFile.Close()

	// Concatenate all parts in order
	for _, partNum := range partNumbers {
		partPath := filepath.Join(info.partsDir, fmt.Sprintf("%d", partNum))
		partFile, err := os.Open(partPath)
		if err != nil {
			return &filekit.PathError{
				Op:   "complete-upload",
				Path: info.path,
				Err:  fmt.Errorf("failed to open part %d: %w", partNum, err),
			}
		}

		_, err = io.Copy(targetFile, partFile)
		partFile.Close()
		if err != nil {
			return &filekit.PathError{
				Op:   "complete-upload",
				Path: info.path,
				Err:  fmt.Errorf("failed to write part %d: %w", partNum, err),
			}
		}
	}

	return nil
}

// AbortUpload cancels a chunked upload and cleans up temporary files.
func (a *Adapter) AbortUpload(ctx context.Context, uploadID string) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	// Get and remove upload info
	uploadRegistry.Lock()
	info, ok := uploadRegistry.uploads[uploadID]
	if ok {
		delete(uploadRegistry.uploads, uploadID)
	}
	uploadRegistry.Unlock()

	if !ok {
		return &filekit.PathError{
			Op:   "abort-upload",
			Path: uploadID,
			Err:  fmt.Errorf("upload not found: %s", uploadID),
		}
	}

	// Clean up parts directory
	if err := os.RemoveAll(info.partsDir); err != nil {
		return &filekit.PathError{
			Op:   "abort-upload",
			Path: uploadID,
			Err:  err,
		}
	}

	return nil
}

// Ensure Adapter implements interfaces
var (
	_ filekit.FileSystem      = (*Adapter)(nil)
	_ filekit.FileReader      = (*Adapter)(nil)
	_ filekit.FileWriter      = (*Adapter)(nil)
	_ filekit.CanCopy         = (*Adapter)(nil)
	_ filekit.CanMove         = (*Adapter)(nil)
	_ filekit.CanChecksum     = (*Adapter)(nil)
	_ filekit.CanWatch        = (*Adapter)(nil)
	_ filekit.ChunkedUploader = (*Adapter)(nil)
)
