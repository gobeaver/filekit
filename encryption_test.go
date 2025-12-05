package filekit

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"io"
	"strings"
	"testing"
)

// encryptionTestFS is a simple in-memory filesystem for encryption tests.
type encryptionTestFS struct {
	files map[string][]byte
}

func newEncryptionTestFS() *encryptionTestFS {
	return &encryptionTestFS{files: make(map[string][]byte)}
}

func (m *encryptionTestFS) Write(ctx context.Context, path string, r io.Reader, opts ...Option) (*WriteResult, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	m.files[path] = data
	return &WriteResult{BytesWritten: int64(len(data))}, nil
}

func (m *encryptionTestFS) Read(ctx context.Context, path string) (io.ReadCloser, error) {
	data, ok := m.files[path]
	if !ok {
		return nil, ErrNotExist
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func (m *encryptionTestFS) ReadAll(ctx context.Context, path string) ([]byte, error) {
	data, ok := m.files[path]
	if !ok {
		return nil, ErrNotExist
	}
	return data, nil
}

func (m *encryptionTestFS) Delete(ctx context.Context, path string) error {
	delete(m.files, path)
	return nil
}

func (m *encryptionTestFS) FileExists(ctx context.Context, path string) (bool, error) {
	_, ok := m.files[path]
	return ok, nil
}

func (m *encryptionTestFS) DirExists(ctx context.Context, path string) (bool, error) {
	return false, nil
}

func (m *encryptionTestFS) Stat(ctx context.Context, path string) (*FileInfo, error) {
	data, ok := m.files[path]
	if !ok {
		return nil, ErrNotExist
	}
	return &FileInfo{Path: path, Size: int64(len(data))}, nil
}

func (m *encryptionTestFS) ListContents(ctx context.Context, path string, recursive bool) ([]FileInfo, error) {
	return nil, nil
}

func (m *encryptionTestFS) CreateDir(ctx context.Context, path string) error {
	return nil
}

func (m *encryptionTestFS) DeleteDir(ctx context.Context, path string) error {
	return nil
}

func generateKey(t *testing.T) []byte {
	t.Helper()
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}
	return key
}

func TestNewEncryptedFS(t *testing.T) {
	fs := newEncryptionTestFS()

	t.Run("valid key", func(t *testing.T) {
		key := generateKey(t)
		encFS, err := NewEncryptedFS(fs, key)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if encFS == nil {
			t.Fatal("expected non-nil EncryptedFS")
		}
	})

	t.Run("invalid key length - too short", func(t *testing.T) {
		key := make([]byte, 16)
		_, err := NewEncryptedFS(fs, key)
		if !errors.Is(err, ErrInvalidKey) {
			t.Fatalf("expected ErrInvalidKey, got %v", err)
		}
	})

	t.Run("invalid key length - too long", func(t *testing.T) {
		key := make([]byte, 64)
		_, err := NewEncryptedFS(fs, key)
		if !errors.Is(err, ErrInvalidKey) {
			t.Fatalf("expected ErrInvalidKey, got %v", err)
		}
	})

	t.Run("nil key", func(t *testing.T) {
		_, err := NewEncryptedFS(fs, nil)
		if !errors.Is(err, ErrInvalidKey) {
			t.Fatalf("expected ErrInvalidKey, got %v", err)
		}
	})

	t.Run("custom chunk size", func(t *testing.T) {
		key := generateKey(t)
		encFS, err := NewEncryptedFS(fs, key, WithChunkSize(1024))
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if encFS.chunkSize != 1024 {
			t.Fatalf("expected chunk size 1024, got %d", encFS.chunkSize)
		}
	})

	t.Run("chunk size too small", func(t *testing.T) {
		key := generateKey(t)
		_, err := NewEncryptedFS(fs, key, WithChunkSize(512))
		if !errors.Is(err, ErrChunkSizeTooSmall) {
			t.Fatalf("expected ErrChunkSizeTooSmall, got %v", err)
		}
	})

	t.Run("chunk size too large", func(t *testing.T) {
		key := generateKey(t)
		_, err := NewEncryptedFS(fs, key, WithChunkSize(32*1024*1024))
		if !errors.Is(err, ErrChunkSizeTooLarge) {
			t.Fatalf("expected ErrChunkSizeTooLarge, got %v", err)
		}
	})
}

func TestEncryptionRoundtrip(t *testing.T) {
	ctx := context.Background()

	testCases := []struct {
		name      string
		data      []byte
		chunkSize int
	}{
		{
			name:      "empty file",
			data:      []byte{},
			chunkSize: defaultChunkSize,
		},
		{
			name:      "small file",
			data:      []byte("Hello, World!"),
			chunkSize: defaultChunkSize,
		},
		{
			name:      "exact chunk size",
			data:      bytes.Repeat([]byte("A"), defaultChunkSize),
			chunkSize: defaultChunkSize,
		},
		{
			name:      "chunk size plus one",
			data:      bytes.Repeat([]byte("B"), defaultChunkSize+1),
			chunkSize: defaultChunkSize,
		},
		{
			name:      "chunk size minus one",
			data:      bytes.Repeat([]byte("C"), defaultChunkSize-1),
			chunkSize: defaultChunkSize,
		},
		{
			name:      "multiple chunks",
			data:      bytes.Repeat([]byte("D"), defaultChunkSize*3+500),
			chunkSize: defaultChunkSize,
		},
		{
			name:      "small chunk size",
			data:      bytes.Repeat([]byte("E"), 10000),
			chunkSize: 1024,
		},
		{
			name:      "binary data",
			data:      generateRandomData(t, 100000),
			chunkSize: defaultChunkSize,
		},
		{
			name:      "large file multiple chunks",
			data:      generateRandomData(t, 500000),
			chunkSize: defaultChunkSize,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			fs := newEncryptionTestFS()
			key := generateKey(t)

			encFS, err := NewEncryptedFS(fs, key, WithChunkSize(tc.chunkSize))
			if err != nil {
				t.Fatalf("failed to create encrypted fs: %v", err)
			}

			// Write encrypted data
			_, err = encFS.Write(ctx, "test.dat", bytes.NewReader(tc.data))
			if err != nil {
				t.Fatalf("failed to write: %v", err)
			}

			// Verify encrypted data is different from plaintext (unless empty)
			if len(tc.data) > 0 {
				encryptedData := fs.files["test.dat"]
				if bytes.Equal(encryptedData, tc.data) {
					t.Error("encrypted data should be different from plaintext")
				}
			}

			// Read and decrypt
			decrypted, err := encFS.ReadAll(ctx, "test.dat")
			if err != nil {
				t.Fatalf("failed to read: %v", err)
			}

			// Verify roundtrip
			if !bytes.Equal(decrypted, tc.data) {
				t.Errorf("roundtrip failed: got %d bytes, want %d bytes", len(decrypted), len(tc.data))
				if len(tc.data) < 100 && len(decrypted) < 100 {
					t.Errorf("data mismatch: got %q, want %q", decrypted, tc.data)
				}
			}
		})
	}
}

func TestEncryptionWithStreamingRead(t *testing.T) {
	ctx := context.Background()
	fs := newEncryptionTestFS()
	key := generateKey(t)

	encFS, err := NewEncryptedFS(fs, key, WithChunkSize(1024))
	if err != nil {
		t.Fatalf("failed to create encrypted fs: %v", err)
	}

	// Write data that spans multiple chunks
	originalData := generateRandomData(t, 5000)
	_, err = encFS.Write(ctx, "stream.dat", bytes.NewReader(originalData))
	if err != nil {
		t.Fatalf("failed to write: %v", err)
	}

	// Read in small chunks to test streaming
	reader, err := encFS.Read(ctx, "stream.dat")
	if err != nil {
		t.Fatalf("failed to open read: %v", err)
	}
	defer reader.Close()

	var result []byte
	buf := make([]byte, 100) // Small buffer to test streaming
	for {
		n, err := reader.Read(buf)
		if n > 0 {
			result = append(result, buf[:n]...)
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("read error: %v", err)
		}
	}

	if !bytes.Equal(result, originalData) {
		t.Errorf("streaming read failed: got %d bytes, want %d bytes", len(result), len(originalData))
	}
}

func TestEncryptionWrongKey(t *testing.T) {
	ctx := context.Background()
	fs := newEncryptionTestFS()

	key1 := generateKey(t)
	key2 := generateKey(t)

	encFS1, _ := NewEncryptedFS(fs, key1)
	encFS2, _ := NewEncryptedFS(fs, key2)

	// Write with key1
	originalData := []byte("Secret message")
	_, err := encFS1.Write(ctx, "secret.dat", bytes.NewReader(originalData))
	if err != nil {
		t.Fatalf("failed to write: %v", err)
	}

	// Try to read with key2
	_, err = encFS2.ReadAll(ctx, "secret.dat")
	if err == nil {
		t.Fatal("expected decryption error with wrong key")
	}

	var pathErr *PathError
	if errors.As(err, &pathErr) {
		if pathErr.Code != ErrCodeDataCorrupted {
			t.Errorf("expected ErrCodeDataCorrupted, got %v", pathErr.Code)
		}
	}
}

func TestEncryptionCorruptedData(t *testing.T) {
	ctx := context.Background()
	fs := newEncryptionTestFS()
	key := generateKey(t)

	encFS, _ := NewEncryptedFS(fs, key)

	// Write valid encrypted data
	_, err := encFS.Write(ctx, "test.dat", bytes.NewReader([]byte("Test data")))
	if err != nil {
		t.Fatalf("failed to write: %v", err)
	}

	t.Run("corrupted header version", func(t *testing.T) {
		// Corrupt the version byte
		data := make([]byte, len(fs.files["test.dat"]))
		copy(data, fs.files["test.dat"])
		data[0] = 99 // Invalid version
		fs.files["corrupted.dat"] = data

		_, err := encFS.ReadAll(ctx, "corrupted.dat")
		if err == nil {
			t.Fatal("expected error for corrupted version")
		}
		if !errors.Is(err, ErrUnsupportedVersion) {
			var pathErr *PathError
			if errors.As(err, &pathErr) {
				if !errors.Is(pathErr.Err, ErrUnsupportedVersion) {
					t.Errorf("expected ErrUnsupportedVersion, got %v", err)
				}
			}
		}
	})

	t.Run("truncated file", func(t *testing.T) {
		// Truncate the file
		data := fs.files["test.dat"][:10]
		fs.files["truncated.dat"] = data

		_, err := encFS.ReadAll(ctx, "truncated.dat")
		if err == nil {
			t.Fatal("expected error for truncated file")
		}
	})

	t.Run("corrupted ciphertext", func(t *testing.T) {
		// Corrupt ciphertext
		data := make([]byte, len(fs.files["test.dat"]))
		copy(data, fs.files["test.dat"])
		if len(data) > 30 {
			data[25] ^= 0xFF // Flip bits in ciphertext
		}
		fs.files["corrupted_cipher.dat"] = data

		_, err := encFS.ReadAll(ctx, "corrupted_cipher.dat")
		if err == nil {
			t.Fatal("expected error for corrupted ciphertext")
		}
	})
}

func TestEncryptionContextCancellation(t *testing.T) {
	fs := newEncryptionTestFS()
	key := generateKey(t)

	encFS, _ := NewEncryptedFS(fs, key)

	t.Run("cancelled context on write", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		_, err := encFS.Write(ctx, "test.dat", strings.NewReader("test"))
		if err == nil {
			t.Fatal("expected error for cancelled context")
		}
	})

	t.Run("cancelled context on read", func(t *testing.T) {
		// First write with valid context
		_, err := encFS.Write(context.Background(), "test.dat", strings.NewReader("test data"))
		if err != nil {
			t.Fatalf("failed to write: %v", err)
		}

		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		_, err = encFS.Read(ctx, "test.dat")
		if err == nil {
			t.Fatal("expected error for cancelled context")
		}
	})
}

func TestEncryptionFileOperations(t *testing.T) {
	ctx := context.Background()
	fs := newEncryptionTestFS()
	key := generateKey(t)

	encFS, _ := NewEncryptedFS(fs, key)

	// Write a file
	_, err := encFS.Write(ctx, "test.dat", strings.NewReader("test content"))
	if err != nil {
		t.Fatalf("failed to write: %v", err)
	}

	t.Run("FileExists", func(t *testing.T) {
		exists, err := encFS.FileExists(ctx, "test.dat")
		if err != nil {
			t.Fatalf("FileExists error: %v", err)
		}
		if !exists {
			t.Error("expected file to exist")
		}

		exists, err = encFS.FileExists(ctx, "nonexistent.dat")
		if err != nil {
			t.Fatalf("FileExists error: %v", err)
		}
		if exists {
			t.Error("expected file to not exist")
		}
	})

	t.Run("Delete", func(t *testing.T) {
		err := encFS.Delete(ctx, "test.dat")
		if err != nil {
			t.Fatalf("Delete error: %v", err)
		}

		exists, _ := encFS.FileExists(ctx, "test.dat")
		if exists {
			t.Error("file should be deleted")
		}
	})

	t.Run("CreateDir and DeleteDir", func(t *testing.T) {
		err := encFS.CreateDir(ctx, "testdir")
		if err != nil {
			t.Fatalf("CreateDir error: %v", err)
		}

		err = encFS.DeleteDir(ctx, "testdir")
		if err != nil {
			t.Fatalf("DeleteDir error: %v", err)
		}
	})
}

func TestEncryptionUnderlying(t *testing.T) {
	fs := newEncryptionTestFS()
	key := generateKey(t)

	encFS, _ := NewEncryptedFS(fs, key)

	underlying := encFS.Underlying()
	if underlying != fs {
		t.Error("Underlying() should return the wrapped filesystem")
	}
}

func TestEncryptionInterfaceCompliance(t *testing.T) {
	fs := newEncryptionTestFS()
	key := generateKey(t)

	encFS, _ := NewEncryptedFS(fs, key)

	// These compile-time checks are in encryption.go, but test runtime too
	var _ FileSystem = encFS
	var _ FileReader = encFS
	var _ FileWriter = encFS
}

func TestEncryptedReaderClose(t *testing.T) {
	ctx := context.Background()
	fs := newEncryptionTestFS()
	key := generateKey(t)

	encFS, _ := NewEncryptedFS(fs, key)

	_, err := encFS.Write(ctx, "test.dat", strings.NewReader("test content"))
	if err != nil {
		t.Fatalf("failed to write: %v", err)
	}

	reader, err := encFS.Read(ctx, "test.dat")
	if err != nil {
		t.Fatalf("failed to read: %v", err)
	}

	// Close should work
	err = reader.Close()
	if err != nil {
		t.Fatalf("Close error: %v", err)
	}

	// Double close should be safe
	err = reader.Close()
	if err != nil {
		t.Fatalf("Double close error: %v", err)
	}

	// Read after close should error
	buf := make([]byte, 10)
	_, err = reader.Read(buf)
	if !errors.Is(err, ErrClosed) {
		t.Errorf("expected ErrClosed after close, got %v", err)
	}
}

func generateRandomData(t *testing.T, size int) []byte {
	t.Helper()
	data := make([]byte, size)
	if _, err := rand.Read(data); err != nil {
		t.Fatalf("failed to generate random data: %v", err)
	}
	return data
}

// Benchmark tests
func BenchmarkEncryptionWrite(b *testing.B) {
	ctx := context.Background()
	fs := newEncryptionTestFS()
	key := make([]byte, 32)
	_, _ = rand.Read(key)

	encFS, _ := NewEncryptedFS(fs, key)
	data := make([]byte, 1024*1024) // 1MB
	_, _ = rand.Read(data)

	b.ResetTimer()
	b.SetBytes(int64(len(data)))

	for i := 0; i < b.N; i++ {
		_, _ = encFS.Write(ctx, "bench.dat", bytes.NewReader(data))
	}
}

func BenchmarkEncryptionRead(b *testing.B) {
	ctx := context.Background()
	fs := newEncryptionTestFS()
	key := make([]byte, 32)
	_, _ = rand.Read(key)

	encFS, _ := NewEncryptedFS(fs, key)
	data := make([]byte, 1024*1024) // 1MB
	_, _ = rand.Read(data)

	_, _ = encFS.Write(ctx, "bench.dat", bytes.NewReader(data))

	b.ResetTimer()
	b.SetBytes(int64(len(data)))

	for i := 0; i < b.N; i++ {
		_, _ = encFS.ReadAll(ctx, "bench.dat")
	}
}

func BenchmarkEncryptionRoundtrip(b *testing.B) {
	ctx := context.Background()
	fs := newEncryptionTestFS()
	key := make([]byte, 32)
	_, _ = rand.Read(key)

	encFS, _ := NewEncryptedFS(fs, key)
	data := make([]byte, 1024*1024) // 1MB
	_, _ = rand.Read(data)

	b.ResetTimer()
	b.SetBytes(int64(len(data)) * 2) // Write + Read

	for i := 0; i < b.N; i++ {
		_, _ = encFS.Write(ctx, "bench.dat", bytes.NewReader(data))
		_, _ = encFS.ReadAll(ctx, "bench.dat")
	}
}
