package filekit

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
)

// Encryption format version for forward compatibility.
const encryptionFormatVersion byte = 1

// Default chunk size for encryption (64KB plaintext per chunk).
// Each chunk will be slightly larger after encryption due to GCM overhead.
const defaultChunkSize = 64 * 1024

// Encryption errors.
var (
	ErrInvalidKey           = errors.New("encryption key must be 32 bytes for AES-256")
	ErrInvalidFormat        = errors.New("invalid encrypted file format")
	ErrUnsupportedVersion   = errors.New("unsupported encryption format version")
	ErrDecryptionFailed     = errors.New("decryption failed: data may be corrupted or key is wrong")
	ErrEncryptionFailed     = errors.New("encryption failed")
	ErrTruncatedFile        = errors.New("encrypted file is truncated")
	ErrInvalidChunkSize     = errors.New("invalid chunk size in encrypted file")
	ErrContextCanceled      = errors.New("operation canceled")
	ErrInvalidNonceSize     = errors.New("invalid nonce size")
	ErrChunkSizeTooSmall    = errors.New("chunk size must be at least 1024 bytes")
	ErrChunkSizeTooLarge    = errors.New("chunk size must be at most 16MB")
	ErrInvalidChunkSequence = errors.New("chunk sequence number mismatch")
)

// Buffer pools for reducing allocations.
var (
	// Pool for plaintext buffers (used during encryption).
	plaintextPool = sync.Pool{
		New: func() interface{} {
			buf := make([]byte, defaultChunkSize)
			return &buf
		},
	}
)

// EncryptedFS wraps a FileSystem to provide transparent AES-256-GCM encryption.
//
// File format (version 1):
//
//	Header (17 bytes):
//	  - Version (1 byte): Format version (currently 1)
//	  - Chunk size (4 bytes): Big-endian uint32, plaintext chunk size
//	  - Base nonce (12 bytes): Random nonce used to derive per-chunk nonces
//
//	Chunks (repeated until EOF):
//	  - Chunk length (4 bytes): Big-endian uint32, length of encrypted chunk
//	  - Chunk sequence (4 bytes): Big-endian uint32, chunk number (for nonce derivation)
//	  - Encrypted data (variable): GCM-encrypted chunk with 16-byte auth tag
//
// Security properties:
//   - Each chunk uses a unique nonce derived from: base_nonce XOR chunk_sequence
//   - GCM provides authenticated encryption (confidentiality + integrity)
//   - Chunk sequence prevents chunk reordering attacks
//   - Format version allows future algorithm upgrades
type EncryptedFS struct {
	fs        FileSystem
	key       []byte
	chunkSize int
}

// EncryptedFSOption configures an EncryptedFS.
type EncryptedFSOption func(*EncryptedFS)

// WithChunkSize sets the chunk size for encryption.
// Must be between 1KB and 16MB. Default is 64KB.
func WithChunkSize(size int) EncryptedFSOption {
	return func(e *EncryptedFS) {
		e.chunkSize = size
	}
}

// NewEncryptedFS creates a new encrypted filesystem wrapper.
//
// The key must be exactly 32 bytes for AES-256.
// Returns an error if the key is invalid.
//
// Example:
//
//	key := make([]byte, 32)
//	if _, err := rand.Read(key); err != nil {
//	    return err
//	}
//	encFS, err := filekit.NewEncryptedFS(fs, key)
//	if err != nil {
//	    return err
//	}
func NewEncryptedFS(fs FileSystem, key []byte, opts ...EncryptedFSOption) (*EncryptedFS, error) {
	if len(key) != 32 {
		return nil, ErrInvalidKey
	}

	e := &EncryptedFS{
		fs:        fs,
		key:       make([]byte, 32),
		chunkSize: defaultChunkSize,
	}

	// Copy key to prevent external modification.
	copy(e.key, key)

	// Apply options.
	for _, opt := range opts {
		opt(e)
	}

	// Validate chunk size.
	if e.chunkSize < 1024 {
		return nil, ErrChunkSizeTooSmall
	}
	if e.chunkSize > 16*1024*1024 {
		return nil, ErrChunkSizeTooLarge
	}

	return e, nil
}

// Write encrypts content and writes it to the underlying filesystem.
func (e *EncryptedFS) Write(ctx context.Context, path string, content io.Reader, options ...Option) (*WriteResult, error) {
	// Check context before starting.
	if err := ctx.Err(); err != nil {
		return nil, WrapPath(err, "encrypt", path, ErrCodeAborted, "context canceled")
	}

	// Create cipher.
	block, err := aes.NewCipher(e.key)
	if err != nil {
		return nil, WrapPath(fmt.Errorf("%w: cipher creation failed: %w", ErrEncryptionFailed, err),
			"encrypt", path, ErrCodeInternal, "cipher creation failed")
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, WrapPath(fmt.Errorf("%w: GCM creation failed: %w", ErrEncryptionFailed, err),
			"encrypt", path, ErrCodeInternal, "GCM creation failed")
	}

	// Generate base nonce.
	baseNonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, baseNonce); err != nil {
		return nil, WrapPath(fmt.Errorf("%w: failed to generate nonce: %w", ErrEncryptionFailed, err),
			"encrypt", path, ErrCodeInternal, "nonce generation failed")
	}

	// Create pipe for streaming encrypted data.
	pr, pw := io.Pipe()

	// Channel to communicate errors from goroutine.
	errChan := make(chan error, 1)

	go func() {
		var encryptErr error
		defer func() {
			if encryptErr != nil {
				pw.CloseWithError(encryptErr)
			} else {
				pw.Close()
			}
			errChan <- encryptErr
		}()

		// Write header.
		header := make([]byte, 17)
		header[0] = encryptionFormatVersion
		binary.BigEndian.PutUint32(header[1:5], uint32(e.chunkSize)) //nolint:gosec // chunkSize is validated to be <= 16MB in WithChunkSize
		copy(header[5:17], baseNonce)

		if _, err := pw.Write(header); err != nil {
			encryptErr = err
			return
		}

		// Get buffer from pool.
		plaintextBufPtr := plaintextPool.Get().(*[]byte)
		plaintextBuf := *plaintextBufPtr
		// Ensure buffer is large enough for our chunk size.
		if len(plaintextBuf) < e.chunkSize {
			plaintextBuf = make([]byte, e.chunkSize)
			*plaintextBufPtr = plaintextBuf
		}
		defer plaintextPool.Put(plaintextBufPtr)

		// Pre-allocate ciphertext buffer (plaintext + GCM overhead).
		ciphertextBuf := make([]byte, 0, e.chunkSize+gcm.Overhead())

		// Chunk header buffer (4 bytes length + 4 bytes sequence).
		chunkHeader := make([]byte, 8)

		// Nonce buffer for per-chunk nonce derivation.
		chunkNonce := make([]byte, gcm.NonceSize())

		var chunkSeq uint32 = 0

		for {
			// Check context.
			select {
			case <-ctx.Done():
				encryptErr = ctx.Err()
				return
			default:
			}

			// Read plaintext chunk.
			n, readErr := io.ReadFull(content, plaintextBuf[:e.chunkSize])
			if readErr != nil && readErr != io.EOF && readErr != io.ErrUnexpectedEOF {
				encryptErr = readErr
				return
			}

			if n == 0 {
				break
			}

			// Derive per-chunk nonce: baseNonce XOR chunkSeq.
			copy(chunkNonce, baseNonce)
			binary.BigEndian.PutUint32(chunkNonce[len(chunkNonce)-4:], chunkSeq)

			// Encrypt chunk (reuse ciphertext buffer).
			ciphertextBuf = gcm.Seal(ciphertextBuf[:0], chunkNonce, plaintextBuf[:n], nil)

			// Write chunk header.
			binary.BigEndian.PutUint32(chunkHeader[0:4], uint32(len(ciphertextBuf))) //nolint:gosec // ciphertextBuf is bounded by chunkSize + GCM overhead
			binary.BigEndian.PutUint32(chunkHeader[4:8], chunkSeq)

			if _, err := pw.Write(chunkHeader); err != nil {
				encryptErr = err
				return
			}

			// Write encrypted chunk.
			if _, err := pw.Write(ciphertextBuf); err != nil {
				encryptErr = err
				return
			}

			chunkSeq++

			if readErr == io.EOF || readErr == io.ErrUnexpectedEOF {
				break
			}
		}
	}()

	// Write to underlying filesystem.
	result, writeErr := e.fs.Write(ctx, path, pr, options...)

	// Wait for encryption goroutine to finish and check for errors.
	encryptErr := <-errChan

	if writeErr != nil {
		return nil, writeErr
	}
	if encryptErr != nil {
		return nil, WrapPath(encryptErr, "encrypt", path, ErrCodeInternal, "encryption failed")
	}

	return result, nil
}

// Read decrypts and returns file content from the underlying filesystem.
func (e *EncryptedFS) Read(ctx context.Context, path string) (io.ReadCloser, error) {
	// Check context before starting.
	if err := ctx.Err(); err != nil {
		return nil, WrapPath(err, "decrypt", path, ErrCodeAborted, "context canceled")
	}

	// Read encrypted content.
	encryptedReader, err := e.fs.Read(ctx, path)
	if err != nil {
		return nil, err
	}

	// Create decrypting reader.
	decReader, err := newDecryptingReader(ctx, encryptedReader, e.key, path)
	if err != nil {
		encryptedReader.Close()
		return nil, err
	}

	return decReader, nil
}

// decryptingReader implements io.ReadCloser for streaming decryption.
type decryptingReader struct {
	ctx        context.Context
	source     io.ReadCloser
	gcm        cipher.AEAD
	baseNonce  []byte
	chunkSize  int
	chunkSeq   uint32
	decrypted  *bytes.Buffer // Buffer for decrypted data not yet read.
	path       string
	closed     bool
	cipherBuf  []byte // Reusable buffer for reading ciphertext.
	chunkNonce []byte // Reusable buffer for nonce derivation.
}

// newDecryptingReader creates a new decrypting reader.
func newDecryptingReader(ctx context.Context, source io.ReadCloser, key []byte, path string) (*decryptingReader, error) {
	// Create cipher.
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, WrapPath(fmt.Errorf("%w: cipher creation failed: %w", ErrDecryptionFailed, err),
			"decrypt", path, ErrCodeInternal, "cipher creation failed")
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, WrapPath(fmt.Errorf("%w: GCM creation failed: %w", ErrDecryptionFailed, err),
			"decrypt", path, ErrCodeInternal, "GCM creation failed")
	}

	// Read header (17 bytes).
	header := make([]byte, 17)
	if _, err := io.ReadFull(source, header); err != nil {
		if err == io.EOF || err == io.ErrUnexpectedEOF {
			return nil, WrapPath(ErrTruncatedFile, "decrypt", path, ErrCodeIntegrity, "encrypted file is truncated")
		}
		return nil, WrapPath(err, "decrypt", path, ErrCodeInternal, "failed to read header")
	}

	// Parse header.
	version := header[0]
	if version != encryptionFormatVersion {
		return nil, WrapPath(fmt.Errorf("%w: got version %d, expected %d", ErrUnsupportedVersion, version, encryptionFormatVersion),
			"decrypt", path, ErrCodeIntegrity, "unsupported encryption version")
	}

	chunkSize := int(binary.BigEndian.Uint32(header[1:5]))
	if chunkSize < 1024 || chunkSize > 16*1024*1024 {
		return nil, WrapPath(ErrInvalidChunkSize, "decrypt", path, ErrCodeIntegrity, "invalid chunk size")
	}

	baseNonce := make([]byte, gcm.NonceSize())
	copy(baseNonce, header[5:17])

	return &decryptingReader{
		ctx:        ctx,
		source:     source,
		gcm:        gcm,
		baseNonce:  baseNonce,
		chunkSize:  chunkSize,
		chunkSeq:   0,
		decrypted:  bytes.NewBuffer(nil),
		path:       path,
		cipherBuf:  make([]byte, chunkSize+gcm.Overhead()),
		chunkNonce: make([]byte, gcm.NonceSize()),
	}, nil
}

// Read implements io.Reader.
func (d *decryptingReader) Read(p []byte) (int, error) {
	if d.closed {
		return 0, ErrClosed
	}

	// Check context.
	select {
	case <-d.ctx.Done():
		return 0, d.ctx.Err()
	default:
	}

	// Return buffered data first.
	if d.decrypted.Len() > 0 {
		return d.decrypted.Read(p)
	}

	// Read next chunk.
	if err := d.decryptNextChunk(); err != nil {
		if errors.Is(err, io.EOF) {
			return 0, io.EOF
		}
		return 0, err
	}

	return d.decrypted.Read(p)
}

// decryptNextChunk reads and decrypts the next chunk from source.
func (d *decryptingReader) decryptNextChunk() error {
	// Read chunk header (8 bytes: 4 length + 4 sequence).
	chunkHeader := make([]byte, 8)
	_, err := io.ReadFull(d.source, chunkHeader)
	if err != nil {
		if errors.Is(err, io.EOF) {
			return io.EOF
		}
		if errors.Is(err, io.ErrUnexpectedEOF) {
			return WrapPath(ErrTruncatedFile, "decrypt", d.path, ErrCodeIntegrity, "chunk header truncated")
		}
		return WrapPath(err, "decrypt", d.path, ErrCodeInternal, "failed to read chunk header")
	}

	chunkLen := int(binary.BigEndian.Uint32(chunkHeader[0:4]))
	chunkSeq := binary.BigEndian.Uint32(chunkHeader[4:8])

	// Verify chunk sequence.
	if chunkSeq != d.chunkSeq {
		return WrapPath(fmt.Errorf("%w: expected %d, got %d", ErrInvalidChunkSequence, d.chunkSeq, chunkSeq),
			"decrypt", d.path, ErrCodeIntegrity, "invalid chunk sequence")
	}

	// Validate chunk length.
	maxChunkLen := d.chunkSize + d.gcm.Overhead()
	if chunkLen <= 0 || chunkLen > maxChunkLen {
		return WrapPath(fmt.Errorf("%w: chunk length %d exceeds maximum %d", ErrInvalidChunkSize, chunkLen, maxChunkLen),
			"decrypt", d.path, ErrCodeIntegrity, "invalid chunk length")
	}

	// Read encrypted chunk.
	ciphertext := d.cipherBuf[:chunkLen]
	if _, err := io.ReadFull(d.source, ciphertext); err != nil {
		if err == io.EOF || err == io.ErrUnexpectedEOF {
			return WrapPath(ErrTruncatedFile, "decrypt", d.path, ErrCodeIntegrity, "chunk data truncated")
		}
		return WrapPath(err, "decrypt", d.path, ErrCodeInternal, "failed to read chunk data")
	}

	// Derive per-chunk nonce.
	copy(d.chunkNonce, d.baseNonce)
	binary.BigEndian.PutUint32(d.chunkNonce[len(d.chunkNonce)-4:], chunkSeq)

	// Decrypt chunk.
	plaintext, err := d.gcm.Open(nil, d.chunkNonce, ciphertext, nil)
	if err != nil {
		return WrapPath(ErrDecryptionFailed, "decrypt", d.path, ErrCodeIntegrity, "decryption failed")
	}

	// Store decrypted data.
	d.decrypted.Reset()
	d.decrypted.Write(plaintext)

	d.chunkSeq++

	return nil
}

// Close implements io.Closer.
func (d *decryptingReader) Close() error {
	if d.closed {
		return nil
	}
	d.closed = true
	return d.source.Close()
}

// ReadAll reads and decrypts all bytes from a file.
func (e *EncryptedFS) ReadAll(ctx context.Context, path string) ([]byte, error) {
	rc, err := e.Read(ctx, path)
	if err != nil {
		return nil, err
	}
	defer rc.Close()

	return io.ReadAll(rc)
}

// Delete delegates to the underlying filesystem.
func (e *EncryptedFS) Delete(ctx context.Context, path string) error {
	return e.fs.Delete(ctx, path)
}

// FileExists delegates to the underlying filesystem.
func (e *EncryptedFS) FileExists(ctx context.Context, path string) (bool, error) {
	return e.fs.FileExists(ctx, path)
}

// DirExists delegates to the underlying filesystem.
func (e *EncryptedFS) DirExists(ctx context.Context, path string) (bool, error) {
	return e.fs.DirExists(ctx, path)
}

// Stat delegates to the underlying filesystem.
// Note: Size will reflect the encrypted size, not the plaintext size.
func (e *EncryptedFS) Stat(ctx context.Context, path string) (*FileInfo, error) {
	return e.fs.Stat(ctx, path)
}

// ListContents delegates to the underlying filesystem.
func (e *EncryptedFS) ListContents(ctx context.Context, path string, recursive bool) ([]FileInfo, error) {
	return e.fs.ListContents(ctx, path, recursive)
}

// CreateDir delegates to the underlying filesystem.
func (e *EncryptedFS) CreateDir(ctx context.Context, path string) error {
	return e.fs.CreateDir(ctx, path)
}

// DeleteDir delegates to the underlying filesystem.
func (e *EncryptedFS) DeleteDir(ctx context.Context, path string) error {
	return e.fs.DeleteDir(ctx, path)
}

// UploadFile encrypts and uploads a local file.
func (e *EncryptedFS) UploadFile(ctx context.Context, path, localPath string, options ...Option) error {
	file, err := os.Open(localPath)
	if err != nil {
		return WrapPathErr("uploadfile", localPath, err)
	}
	defer file.Close()

	_, err = e.Write(ctx, path, file, options...)
	return err
}

// Underlying returns the wrapped filesystem.
func (e *EncryptedFS) Underlying() FileSystem {
	return e.fs
}

// Verify interface compliance at compile time.
var (
	_ FileSystem = (*EncryptedFS)(nil)
	_ FileReader = (*EncryptedFS)(nil)
	_ FileWriter = (*EncryptedFS)(nil)
)
