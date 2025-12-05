package azure

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/blob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/bloberror"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/blockblob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/container"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/sas"
	"github.com/gobeaver/filekit"
)

// Adapter provides an Azure Blob Storage implementation of filekit.FileSystem
type Adapter struct {
	client        *azblob.Client
	containerName string
	prefix        string
	accountName   string
	accountKey    string
}

// AdapterOption is a function that configures Azure Adapter
type AdapterOption func(*Adapter)

// WithPrefix sets the prefix for Azure blobs
func WithPrefix(prefix string) AdapterOption {
	return func(a *Adapter) {
		// Ensure prefix ends with a slash if it's not empty
		if prefix != "" && !strings.HasSuffix(prefix, "/") {
			prefix += "/"
		}
		a.prefix = prefix
	}
}

// New creates a new Azure Blob Storage filesystem adapter
func New(client *azblob.Client, containerName string, accountName, accountKey string, options ...AdapterOption) *Adapter {
	adapter := &Adapter{
		client:        client,
		containerName: containerName,
		accountName:   accountName,
		accountKey:    accountKey,
	}

	// Apply options
	for _, option := range options {
		option(adapter)
	}

	return adapter
}

// Write implements filekit.FileWriter
func (a *Adapter) Write(ctx context.Context, filePath string, content io.Reader, options ...filekit.Option) (*filekit.WriteResult, error) {
	opts := processOptions(options...)

	// Combine prefix and path
	blobName := path.Join(a.prefix, filePath)

	// Check if blob exists and overwrite is not allowed
	if !opts.Overwrite {
		blobClient := a.client.ServiceClient().NewContainerClient(a.containerName).NewBlobClient(blobName)
		_, err := blobClient.GetProperties(ctx, nil)
		if err == nil {
			return nil, filekit.NewPathError("write", filePath, filekit.ErrExist)
		}
		if !bloberror.HasCode(err, bloberror.BlobNotFound) {
			return nil, mapAzureError("write", filePath, err)
		}
	}

	// Determine content type
	contentType := opts.ContentType
	if contentType == "" {
		contentType = detectContentType(filePath)
	}

	// Read content into buffer (Azure SDK requires content length for some operations)
	data, err := io.ReadAll(content)
	if err != nil {
		return nil, filekit.NewPathError("write", filePath, err)
	}

	// Calculate checksum
	hash := sha256.Sum256(data)
	checksum := hex.EncodeToString(hash[:])

	// Upload options
	uploadOpts := &azblob.UploadBufferOptions{
		HTTPHeaders: &blob.HTTPHeaders{
			BlobContentType: &contentType,
		},
	}

	// Set cache control if provided
	if opts.CacheControl != "" {
		uploadOpts.HTTPHeaders.BlobCacheControl = &opts.CacheControl
	}

	// Set metadata if provided
	if len(opts.Metadata) > 0 {
		metadata := make(map[string]*string, len(opts.Metadata))
		for k, v := range opts.Metadata {
			val := v
			metadata[k] = &val
		}
		uploadOpts.Metadata = metadata
	}

	// Upload the blob
	resp, err := a.client.UploadBuffer(ctx, a.containerName, blobName, data, uploadOpts)
	if err != nil {
		return nil, mapAzureError("write", filePath, err)
	}

	// Get ETag and timestamp from response
	var etag string
	var serverTime time.Time
	if resp.ETag != nil {
		etag = string(*resp.ETag)
	}
	if resp.LastModified != nil {
		serverTime = *resp.LastModified
	} else {
		serverTime = time.Now()
	}

	return &filekit.WriteResult{
		BytesWritten:      int64(len(data)),
		Checksum:          checksum,
		ChecksumAlgorithm: filekit.ChecksumSHA256,
		ETag:              etag,
		ServerTimestamp:   serverTime,
	}, nil
}

// Read implements filekit.FileReader
func (a *Adapter) Read(ctx context.Context, filePath string) (io.ReadCloser, error) {
	blobName := path.Join(a.prefix, filePath)

	// Download the blob
	resp, err := a.client.DownloadStream(ctx, a.containerName, blobName, nil)
	if err != nil {
		return nil, mapAzureError("read", filePath, err)
	}

	return resp.Body, nil
}

// ReadAll reads the entire file and returns its contents as a byte slice
func (a *Adapter) ReadAll(ctx context.Context, path string) ([]byte, error) {
	rc, err := a.Read(ctx, path)
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(rc)
}

// Delete implements filekit.FileSystem
func (a *Adapter) Delete(ctx context.Context, filePath string) error {
	blobName := path.Join(a.prefix, filePath)

	_, err := a.client.DeleteBlob(ctx, a.containerName, blobName, nil)
	if err != nil {
		return mapAzureError("delete", filePath, err)
	}

	return nil
}

// FileExists checks if a file exists (not a directory)
func (a *Adapter) FileExists(ctx context.Context, filePath string) (bool, error) {
	blobName := path.Join(a.prefix, filePath)

	// Ensure it's not a directory marker
	if strings.HasSuffix(blobName, "/") {
		return false, nil
	}

	blobClient := a.client.ServiceClient().NewContainerClient(a.containerName).NewBlobClient(blobName)
	props, err := blobClient.GetProperties(ctx, nil)
	if err != nil {
		if bloberror.HasCode(err, bloberror.BlobNotFound) {
			return false, nil
		}
		return false, mapAzureError("fileexists", filePath, err)
	}

	// Check if it's a directory marker
	if props.ContentType != nil && *props.ContentType == "application/x-directory" {
		return false, nil
	}

	return true, nil
}

// DirExists checks if a directory exists
func (a *Adapter) DirExists(ctx context.Context, dirPath string) (bool, error) {
	// Prepare directory path
	dirPrefix := path.Join(a.prefix, dirPath)
	if !strings.HasSuffix(dirPrefix, "/") {
		dirPrefix += "/"
	}

	containerClient := a.client.ServiceClient().NewContainerClient(a.containerName)

	// Check if directory marker blob exists
	blobClient := containerClient.NewBlobClient(dirPrefix)
	_, err := blobClient.GetProperties(ctx, nil)
	if err == nil {
		return true, nil
	}

	// If directory marker doesn't exist, check if any blobs with this prefix exist
	pager := containerClient.NewListBlobsFlatPager(&container.ListBlobsFlatOptions{
		Prefix:     &dirPrefix,
		MaxResults: ptr(int32(1)),
	})

	if pager.More() {
		resp, err := pager.NextPage(ctx)
		if err != nil {
			return false, mapAzureError("direxists", dirPath, err)
		}
		return len(resp.Segment.BlobItems) > 0, nil
	}

	return false, nil
}

// ptr is a helper function to create a pointer to a value
func ptr[T any](v T) *T {
	return &v
}

// Stat implements filekit.FileReader
func (a *Adapter) Stat(ctx context.Context, filePath string) (*filekit.FileInfo, error) {
	blobName := path.Join(a.prefix, filePath)

	blobClient := a.client.ServiceClient().NewContainerClient(a.containerName).NewBlobClient(blobName)
	props, err := blobClient.GetProperties(ctx, nil)
	if err != nil {
		return nil, mapAzureError("stat", filePath, err)
	}

	// Determine if it's a directory
	isDir := strings.HasSuffix(blobName, "/")

	var size int64
	if props.ContentLength != nil {
		size = *props.ContentLength
	}

	var modTime time.Time
	if props.LastModified != nil {
		modTime = *props.LastModified
	}

	var contentType string
	if props.ContentType != nil {
		contentType = *props.ContentType
	}

	// Convert metadata from *string to string
	metadata := make(map[string]string, len(props.Metadata))
	for k, v := range props.Metadata {
		if v != nil {
			metadata[k] = *v
		}
	}

	return &filekit.FileInfo{
		Name:        filepath.Base(filePath),
		Path:        filePath,
		Size:        size,
		ModTime:     modTime,
		IsDir:       isDir,
		ContentType: contentType,
		Metadata:    metadata,
	}, nil
}

// ListContents lists files and directories at the given path with optional recursion
func (a *Adapter) ListContents(ctx context.Context, dirPath string, recursive bool) ([]filekit.FileInfo, error) {
	// Prepare prefix for listing
	listPrefix := dirPath
	if a.prefix != "" {
		listPrefix = path.Join(a.prefix, dirPath)
	}
	if listPrefix != "" && !strings.HasSuffix(listPrefix, "/") {
		listPrefix += "/"
	}

	containerClient := a.client.ServiceClient().NewContainerClient(a.containerName)

	var files []filekit.FileInfo

	if recursive {
		// Recursive listing - use flat pager
		pager := containerClient.NewListBlobsFlatPager(&container.ListBlobsFlatOptions{
			Prefix: &listPrefix,
		})

		for pager.More() {
			resp, err := pager.NextPage(ctx)
			if err != nil {
				return nil, mapAzureError("listcontents", dirPath, err)
			}

			for _, blobItem := range resp.Segment.BlobItems {
				if blobItem.Name == nil {
					continue
				}

				// Skip the directory itself
				if *blobItem.Name == listPrefix {
					continue
				}

				relativePath := strings.TrimPrefix(*blobItem.Name, listPrefix)
				if relativePath == "" {
					continue
				}

				var size int64
				var modTime time.Time
				var contentType string

				if blobItem.Properties != nil {
					if blobItem.Properties.ContentLength != nil {
						size = *blobItem.Properties.ContentLength
					}
					if blobItem.Properties.LastModified != nil {
						modTime = *blobItem.Properties.LastModified
					}
					if blobItem.Properties.ContentType != nil {
						contentType = *blobItem.Properties.ContentType
					}
				}

				isDir := strings.HasSuffix(*blobItem.Name, "/") || (contentType == "application/x-directory")

				files = append(files, filekit.FileInfo{
					Name:        filepath.Base(relativePath),
					Path:        path.Join(dirPath, relativePath),
					Size:        size,
					ModTime:     modTime,
					IsDir:       isDir,
					ContentType: contentType,
				})
			}
		}
	} else {
		// Non-recursive listing - use hierarchy pager
		pager := containerClient.NewListBlobsHierarchyPager("/", &container.ListBlobsHierarchyOptions{
			Prefix: &listPrefix,
		})

		for pager.More() {
			resp, err := pager.NextPage(ctx)
			if err != nil {
				return nil, mapAzureError("listcontents", dirPath, err)
			}

			// Add directories (blob prefixes)
			for _, blobPrefix := range resp.Segment.BlobPrefixes {
				if blobPrefix.Name == nil {
					continue
				}
				dirName := strings.TrimPrefix(*blobPrefix.Name, listPrefix)
				dirName = strings.TrimSuffix(dirName, "/")
				if dirName == "" {
					continue
				}

				files = append(files, filekit.FileInfo{
					Name:  dirName,
					Path:  path.Join(dirPath, dirName),
					IsDir: true,
				})
			}

			// Add files
			for _, blobItem := range resp.Segment.BlobItems {
				if blobItem.Name == nil {
					continue
				}

				// Skip the directory itself
				if *blobItem.Name == listPrefix {
					continue
				}

				fileName := strings.TrimPrefix(*blobItem.Name, listPrefix)
				if fileName == "" || strings.Contains(fileName, "/") {
					continue
				}

				var size int64
				var modTime time.Time
				var contentType string

				if blobItem.Properties != nil {
					if blobItem.Properties.ContentLength != nil {
						size = *blobItem.Properties.ContentLength
					}
					if blobItem.Properties.LastModified != nil {
						modTime = *blobItem.Properties.LastModified
					}
					if blobItem.Properties.ContentType != nil {
						contentType = *blobItem.Properties.ContentType
					}
				}

				files = append(files, filekit.FileInfo{
					Name:        fileName,
					Path:        path.Join(dirPath, fileName),
					Size:        size,
					ModTime:     modTime,
					IsDir:       false,
					ContentType: contentType,
				})
			}
		}
	}

	return files, nil
}

// CreateDir implements filekit.FileSystem
func (a *Adapter) CreateDir(ctx context.Context, dirPath string) error {
	// Azure Blob Storage doesn't have real directories
	// We create an empty blob with a trailing slash to simulate a directory
	blobName := path.Join(a.prefix, dirPath)
	if !strings.HasSuffix(blobName, "/") {
		blobName += "/"
	}

	contentType := "application/x-directory"
	uploadOpts := &azblob.UploadBufferOptions{
		HTTPHeaders: &blob.HTTPHeaders{
			BlobContentType: &contentType,
		},
	}

	_, err := a.client.UploadBuffer(ctx, a.containerName, blobName, []byte{}, uploadOpts)
	if err != nil {
		return mapAzureError("createdir", dirPath, err)
	}

	return nil
}

// DeleteDir implements filekit.FileSystem
func (a *Adapter) DeleteDir(ctx context.Context, dirPath string) error {
	// Prepare directory path
	dirPrefix := path.Join(a.prefix, dirPath)
	if !strings.HasSuffix(dirPrefix, "/") {
		dirPrefix += "/"
	}

	containerClient := a.client.ServiceClient().NewContainerClient(a.containerName)

	// List all blobs with the prefix
	pager := containerClient.NewListBlobsFlatPager(&container.ListBlobsFlatOptions{
		Prefix: &dirPrefix,
	})

	var found bool
	for pager.More() {
		resp, err := pager.NextPage(ctx)
		if err != nil {
			return mapAzureError("deletedir", dirPath, err)
		}

		for _, blobItem := range resp.Segment.BlobItems {
			if blobItem.Name == nil {
				continue
			}
			found = true

			// Delete the blob
			_, err := a.client.DeleteBlob(ctx, a.containerName, *blobItem.Name, nil)
			if err != nil {
				return mapAzureError("deletedir", dirPath, err)
			}
		}
	}

	if !found {
		return &filekit.PathError{
			Op:   "deletedir",
			Path: dirPath,
			Err:  filekit.ErrNotExist,
		}
	}

	return nil
}

// WriteFile writes a local file to the storage
func (a *Adapter) WriteFile(ctx context.Context, destPath string, localPath string, options ...filekit.Option) (*filekit.WriteResult, error) {
	file, err := os.Open(localPath)
	if err != nil {
		return nil, filekit.NewPathError("writefile", localPath, err)
	}
	defer file.Close()

	// Detect content type if not provided
	opts := processOptions(options...)
	if opts.ContentType == "" {
		contentType := detectContentType(localPath)
		options = append(options, filekit.WithContentType(contentType))
	}

	return a.Write(ctx, destPath, file, options...)
}

// GenerateSASURL generates a SAS URL for accessing a blob
func (a *Adapter) GenerateSASURL(ctx context.Context, filePath string, expiry time.Duration, permissions sas.BlobPermissions) (string, error) {
	if a.accountKey == "" {
		return "", fmt.Errorf("account key required for SAS URL generation")
	}

	blobName := path.Join(a.prefix, filePath)

	// Create shared key credential
	cred, err := azblob.NewSharedKeyCredential(a.accountName, a.accountKey)
	if err != nil {
		return "", mapAzureError("generate-sas", filePath, err)
	}

	// Generate SAS token
	sasQueryParams, err := sas.BlobSignatureValues{
		Protocol:      sas.ProtocolHTTPS,
		StartTime:     time.Now().UTC(),
		ExpiryTime:    time.Now().UTC().Add(expiry),
		Permissions:   permissions.String(),
		ContainerName: a.containerName,
		BlobName:      blobName,
	}.SignWithSharedKey(cred)
	if err != nil {
		return "", mapAzureError("generate-sas", filePath, err)
	}

	// Construct full URL
	blobURL := fmt.Sprintf("https://%s.blob.core.windows.net/%s/%s?%s",
		a.accountName, a.containerName, blobName, sasQueryParams.Encode())

	return blobURL, nil
}

// GenerateDownloadURL generates a SAS URL for downloading a blob
func (a *Adapter) GenerateDownloadURL(ctx context.Context, filePath string, expiry time.Duration) (string, error) {
	return a.GenerateSASURL(ctx, filePath, expiry, sas.BlobPermissions{Read: true})
}

// GenerateUploadURL generates a SAS URL for uploading a blob
func (a *Adapter) GenerateUploadURL(ctx context.Context, filePath string, expiry time.Duration) (string, error) {
	return a.GenerateSASURL(ctx, filePath, expiry, sas.BlobPermissions{Write: true, Create: true})
}

// SignedURL generates a signed URL for downloading a file
func (a *Adapter) SignedURL(ctx context.Context, filePath string, expiry time.Duration) (string, error) {
	return a.GenerateDownloadURL(ctx, filePath, expiry)
}

// SignedUploadURL generates a signed URL for uploading a file
func (a *Adapter) SignedUploadURL(ctx context.Context, filePath string, expiry time.Duration) (string, error) {
	return a.GenerateUploadURL(ctx, filePath, expiry)
}

// processOptions processes the provided options
func processOptions(options ...filekit.Option) *filekit.Options {
	opts := &filekit.Options{}
	for _, option := range options {
		option(opts)
	}
	return opts
}

// detectContentType determines the content type from file extension
func detectContentType(filePath string) string {
	ext := filepath.Ext(filePath)

	// Common extension mappings
	switch strings.ToLower(ext) {
	case ".html", ".htm":
		return "text/html"
	case ".css":
		return "text/css"
	case ".js":
		return "application/javascript"
	case ".json":
		return "application/json"
	case ".xml":
		return "application/xml"
	case ".pdf":
		return "application/pdf"
	case ".zip":
		return "application/zip"
	case ".gz", ".gzip":
		return "application/gzip"
	case ".tar":
		return "application/x-tar"
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".svg":
		return "image/svg+xml"
	case ".ico":
		return "image/x-icon"
	case ".mp3":
		return "audio/mpeg"
	case ".mp4":
		return "video/mp4"
	case ".webm":
		return "video/webm"
	case ".txt":
		return "text/plain"
	case ".csv":
		return "text/csv"
	case ".md":
		return "text/markdown"
	default:
		return "application/octet-stream"
	}
}

// mapAzureError maps Azure errors to filekit errors
func mapAzureError(op, path string, err error) error {
	if bloberror.HasCode(err, bloberror.BlobNotFound) {
		return &filekit.PathError{
			Op:   op,
			Path: path,
			Err:  filekit.ErrNotExist,
		}
	}

	if bloberror.HasCode(err, bloberror.ContainerNotFound) {
		return &filekit.PathError{
			Op:   op,
			Path: path,
			Err:  filekit.ErrNotExist,
		}
	}

	var respErr *azcore.ResponseError
	if errors.As(err, &respErr) {
		if respErr.StatusCode == http.StatusNotFound {
			return &filekit.PathError{
				Op:   op,
				Path: path,
				Err:  filekit.ErrNotExist,
			}
		}
		if respErr.StatusCode == http.StatusForbidden {
			return &filekit.PathError{
				Op:   op,
				Path: path,
				Err:  filekit.ErrPermission,
			}
		}
	}

	return &filekit.PathError{
		Op:   op,
		Path: path,
		Err:  err,
	}
}

// ============================================================================
// Optional Capability Interfaces
// ============================================================================

// Copy implements filekit.CanCopy using Azure's native StartCopyFromURL.
func (a *Adapter) Copy(ctx context.Context, src, dst string) error {
	srcKey := path.Join(a.prefix, src)
	dstKey := path.Join(a.prefix, dst)

	// Generate source URL (need SAS for copy to work)
	srcURL, err := a.GenerateSASURL(ctx, src, 15*time.Minute, sas.BlobPermissions{Read: true})
	if err != nil {
		// If SAS generation fails, try direct URL
		srcURL = fmt.Sprintf("https://%s.blob.core.windows.net/%s/%s",
			a.accountName, a.containerName, srcKey)
	}

	// Start the copy operation
	_, err = a.client.ServiceClient().NewContainerClient(a.containerName).NewBlobClient(dstKey).StartCopyFromURL(ctx, srcURL, nil)
	if err != nil {
		return mapAzureError("copy", src, err)
	}

	return nil
}

// Move implements filekit.CanMove using Azure's copy + delete.
func (a *Adapter) Move(ctx context.Context, src, dst string) error {
	// Copy the blob
	if err := a.Copy(ctx, src, dst); err != nil {
		return err
	}

	// Delete the source
	return a.Delete(ctx, src)
}

// Checksum implements filekit.CanChecksum by downloading and hashing the file.
func (a *Adapter) Checksum(ctx context.Context, filePath string, algorithm filekit.ChecksumAlgorithm) (string, error) {
	reader, err := a.Read(ctx, filePath)
	if err != nil {
		return "", err
	}
	defer reader.Close()

	checksum, err := filekit.CalculateChecksum(reader, algorithm)
	if err != nil {
		return "", &filekit.PathError{Op: "checksum", Path: filePath, Err: err}
	}

	return checksum, nil
}

// Checksums implements filekit.MultiChecksummer for efficient multi-hash calculation.
func (a *Adapter) Checksums(ctx context.Context, filePath string, algorithms []filekit.ChecksumAlgorithm) (map[filekit.ChecksumAlgorithm]string, error) {
	reader, err := a.Read(ctx, filePath)
	if err != nil {
		return nil, err
	}
	defer reader.Close()

	checksums, err := filekit.CalculateChecksums(reader, algorithms)
	if err != nil {
		return nil, &filekit.PathError{Op: "checksums", Path: filePath, Err: err}
	}

	return checksums, nil
}

// ============================================================================
// Watcher Implementation (Polling-based)
// ============================================================================

// Watch implements filekit.CanWatch using a polling approach.
// Azure Blob Storage doesn't have native file system events, so we poll for changes.
// The filter pattern supports glob patterns like "**/*.json", "config/*".
// Default polling interval is 30 seconds.
func (a *Adapter) Watch(ctx context.Context, filter string) (filekit.ChangeToken, error) {
	// Get initial state of matching files
	initialState, err := a.getMatchingFilesState(ctx, filter)
	if err != nil {
		return nil, err
	}

	// Create a polling change token that checks for changes
	token := filekit.NewPollingChangeToken(ctx, filekit.PollingConfig{
		Interval: 30 * time.Second,
		CheckFunc: func() bool {
			currentState, err := a.getMatchingFilesState(ctx, filter)
			if err != nil {
				return false // Can't determine change, don't signal
			}
			return !azureStatesEqual(initialState, currentState)
		},
	})

	return token, nil
}

// azureFileState represents the state of a file for change detection
type azureFileState struct {
	path    string
	modTime time.Time
	size    int64
}

// getMatchingFilesState returns the current state of files matching the filter
func (a *Adapter) getMatchingFilesState(ctx context.Context, filter string) (map[string]azureFileState, error) {
	state := make(map[string]azureFileState)

	// List all blobs with the prefix
	pager := a.client.NewListBlobsFlatPager(a.containerName, &azblob.ListBlobsFlatOptions{
		Prefix: &a.prefix,
	})

	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, &filekit.PathError{Op: "watch", Path: filter, Err: err}
		}

		for _, blob := range page.Segment.BlobItems {
			if blob.Name == nil {
				continue
			}

			// Remove prefix from key to get relative path
			relPath := strings.TrimPrefix(*blob.Name, a.prefix)

			// Check if path matches filter
			if azureMatchesGlobFilter(relPath, filter) {
				var modTime time.Time
				var size int64
				if blob.Properties != nil {
					if blob.Properties.LastModified != nil {
						modTime = *blob.Properties.LastModified
					}
					if blob.Properties.ContentLength != nil {
						size = *blob.Properties.ContentLength
					}
				}
				state[relPath] = azureFileState{
					path:    relPath,
					modTime: modTime,
					size:    size,
				}
			}
		}
	}

	return state, nil
}

// azureStatesEqual checks if two file states are equal
func azureStatesEqual(a, b map[string]azureFileState) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		bv, ok := b[k]
		if !ok {
			return false // File was deleted
		}
		if v.modTime != bv.modTime || v.size != bv.size {
			return false // File was modified
		}
	}
	return true
}

// azureMatchesGlobFilter checks if a path matches a glob pattern
func azureMatchesGlobFilter(filePath, filter string) bool {
	// Handle ** patterns for recursive matching
	if strings.Contains(filter, "**") {
		parts := strings.Split(filter, "**")
		if len(parts) == 2 {
			prefix := strings.TrimSuffix(parts[0], "/")
			suffix := strings.TrimPrefix(parts[1], "/")

			hasPrefix := prefix == "" || strings.HasPrefix(filePath, prefix)
			hasSuffix := suffix == "" || strings.HasSuffix(filePath, suffix)

			return hasPrefix && hasSuffix
		}
	}

	// Simple glob matching
	matched, _ := path.Match(filter, filePath)
	return matched
}

// ============================================================================
// Chunked Upload Implementation (using Azure Block Blobs)
// ============================================================================

// azureUploadInfo stores metadata for an in-progress chunked upload.
type azureUploadInfo struct {
	path     string   // Target blob path
	blockIDs []string // List of block IDs in order
	adapter  *Adapter
	mu       sync.Mutex
}

// azureUploadRegistry is a thread-safe registry for in-progress uploads.
var azureUploadRegistry = struct {
	sync.RWMutex
	uploads map[string]*azureUploadInfo
}{
	uploads: make(map[string]*azureUploadInfo),
}

// generateAzureUploadID creates a unique upload identifier.
func generateAzureUploadID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// generateBlockID creates a base64-encoded block ID for Azure.
// Azure requires block IDs to be base64-encoded and all the same length.
func generateBlockID(partNumber int) string {
	// Use a fixed-width format to ensure consistent block ID lengths
	id := fmt.Sprintf("block-%010d", partNumber)
	return base64.StdEncoding.EncodeToString([]byte(id))
}

// InitiateUpload starts a chunked upload process and returns an upload ID.
// Uses Azure Block Blob's native chunked upload mechanism.
func (a *Adapter) InitiateUpload(ctx context.Context, filePath string) (string, error) {
	// Generate a unique upload ID
	uploadID, err := generateAzureUploadID()
	if err != nil {
		return "", &filekit.PathError{
			Op:   "initiate-upload",
			Path: filePath,
			Err:  err,
		}
	}

	// Store upload info
	azureUploadRegistry.Lock()
	azureUploadRegistry.uploads[uploadID] = &azureUploadInfo{
		path:     filePath,
		blockIDs: make([]string, 0),
		adapter:  a,
	}
	azureUploadRegistry.Unlock()

	return uploadID, nil
}

// UploadPart uploads a part of a file using Azure's StageBlock.
func (a *Adapter) UploadPart(ctx context.Context, uploadID string, partNumber int, data []byte) error {
	// Validate part number
	if partNumber < 1 {
		return &filekit.PathError{
			Op:   "upload-part",
			Path: uploadID,
			Err:  fmt.Errorf("part number must be >= 1, got %d", partNumber),
		}
	}

	// Get upload info
	azureUploadRegistry.RLock()
	info, ok := azureUploadRegistry.uploads[uploadID]
	azureUploadRegistry.RUnlock()

	if !ok {
		return &filekit.PathError{
			Op:   "upload-part",
			Path: uploadID,
			Err:  fmt.Errorf("upload not found: %s", uploadID),
		}
	}

	// Generate block ID
	blockID := generateBlockID(partNumber)

	// Get block blob client
	blobName := path.Join(a.prefix, info.path)
	blockBlobClient := a.client.ServiceClient().NewContainerClient(a.containerName).NewBlockBlobClient(blobName)

	// Stage the block
	_, err := blockBlobClient.StageBlock(ctx, blockID, &readSeekCloser{data: data}, nil)
	if err != nil {
		return &filekit.PathError{
			Op:   "upload-part",
			Path: uploadID,
			Err:  fmt.Errorf("failed to stage block: %w", err),
		}
	}

	// Record the block ID (thread-safe)
	info.mu.Lock()
	// Ensure we have enough space in the slice
	for len(info.blockIDs) < partNumber {
		info.blockIDs = append(info.blockIDs, "")
	}
	info.blockIDs[partNumber-1] = blockID
	info.mu.Unlock()

	return nil
}

// readSeekCloser wraps a byte slice to implement io.ReadSeekCloser
type readSeekCloser struct {
	data   []byte
	offset int64
}

func (r *readSeekCloser) Read(p []byte) (n int, err error) {
	if r.offset >= int64(len(r.data)) {
		return 0, io.EOF
	}
	n = copy(p, r.data[r.offset:])
	r.offset += int64(n)
	return n, nil
}

func (r *readSeekCloser) Seek(offset int64, whence int) (int64, error) {
	var newOffset int64
	switch whence {
	case io.SeekStart:
		newOffset = offset
	case io.SeekCurrent:
		newOffset = r.offset + offset
	case io.SeekEnd:
		newOffset = int64(len(r.data)) + offset
	default:
		return 0, errors.New("invalid whence")
	}
	if newOffset < 0 {
		return 0, errors.New("negative position")
	}
	r.offset = newOffset
	return newOffset, nil
}

func (r *readSeekCloser) Close() error {
	return nil
}

// CompleteUpload finalizes a chunked upload by committing all blocks.
func (a *Adapter) CompleteUpload(ctx context.Context, uploadID string) error {
	// Get and remove upload info
	azureUploadRegistry.Lock()
	info, ok := azureUploadRegistry.uploads[uploadID]
	if ok {
		delete(azureUploadRegistry.uploads, uploadID)
	}
	azureUploadRegistry.Unlock()

	if !ok {
		return &filekit.PathError{
			Op:   "complete-upload",
			Path: uploadID,
			Err:  fmt.Errorf("upload not found: %s", uploadID),
		}
	}

	// Filter out empty block IDs and collect valid ones
	info.mu.Lock()
	var validBlockIDs []string
	for _, id := range info.blockIDs {
		if id != "" {
			validBlockIDs = append(validBlockIDs, id)
		}
	}
	info.mu.Unlock()

	if len(validBlockIDs) == 0 {
		return &filekit.PathError{
			Op:   "complete-upload",
			Path: uploadID,
			Err:  errors.New("no parts uploaded"),
		}
	}

	// Sort block IDs by their part number (they're base64 encoded, but in order)
	sort.Strings(validBlockIDs)

	// Get block blob client
	blobName := path.Join(a.prefix, info.path)
	blockBlobClient := a.client.ServiceClient().NewContainerClient(a.containerName).NewBlockBlobClient(blobName)

	// Commit the block list
	_, err := blockBlobClient.CommitBlockList(ctx, validBlockIDs, &blockblob.CommitBlockListOptions{})
	if err != nil {
		return &filekit.PathError{
			Op:   "complete-upload",
			Path: info.path,
			Err:  fmt.Errorf("failed to commit block list: %w", err),
		}
	}

	return nil
}

// AbortUpload cancels a chunked upload.
// For Azure Block Blobs, uncommitted blocks are automatically cleaned up after 7 days.
func (a *Adapter) AbortUpload(ctx context.Context, uploadID string) error {
	// Get and remove upload info
	azureUploadRegistry.Lock()
	info, ok := azureUploadRegistry.uploads[uploadID]
	if ok {
		delete(azureUploadRegistry.uploads, uploadID)
	}
	azureUploadRegistry.Unlock()

	if !ok {
		return &filekit.PathError{
			Op:   "abort-upload",
			Path: uploadID,
			Err:  fmt.Errorf("upload not found: %s", uploadID),
		}
	}

	// Azure automatically cleans up uncommitted blocks after 7 days
	// We could try to delete any partially committed blob, but it may not exist yet
	blobName := path.Join(a.prefix, info.path)
	_, _ = a.client.DeleteBlob(ctx, a.containerName, blobName, nil)

	return nil
}

// Ensure Adapter implements required and optional interfaces
var (
	_ filekit.FileSystem      = (*Adapter)(nil)
	_ filekit.FileReader      = (*Adapter)(nil)
	_ filekit.FileWriter      = (*Adapter)(nil)
	_ filekit.CanCopy         = (*Adapter)(nil)
	_ filekit.CanMove         = (*Adapter)(nil)
	_ filekit.CanSignURL      = (*Adapter)(nil)
	_ filekit.CanChecksum     = (*Adapter)(nil)
	_ filekit.CanWatch        = (*Adapter)(nil)
	_ filekit.ChunkedUploader = (*Adapter)(nil)
)
