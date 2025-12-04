package local

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/gobeaver/filekit"
)

func TestInitiateUpload(t *testing.T) {
	ctx := context.Background()

	t.Run("returns upload ID", func(t *testing.T) {
		tmpDir := t.TempDir()
		a, err := New(tmpDir)
		if err != nil {
			t.Fatalf("failed to create adapter: %v", err)
		}

		uploadID, err := a.InitiateUpload(ctx, "test.txt")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if uploadID == "" {
			t.Error("expected non-empty upload ID")
		}

		// Cleanup
		_ = a.AbortUpload(ctx, uploadID)
	})

	t.Run("rejects path traversal", func(t *testing.T) {
		tmpDir := t.TempDir()
		a, err := New(tmpDir)
		if err != nil {
			t.Fatalf("failed to create adapter: %v", err)
		}

		_, err = a.InitiateUpload(ctx, "../etc/passwd")
		if err == nil {
			t.Fatal("expected error for path traversal")
		}
	})

	t.Run("respects context cancellation", func(t *testing.T) {
		tmpDir := t.TempDir()
		a, err := New(tmpDir)
		if err != nil {
			t.Fatalf("failed to create adapter: %v", err)
		}

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err = a.InitiateUpload(ctx, "test.txt")
		if err != context.Canceled {
			t.Errorf("expected context.Canceled, got: %v", err)
		}
	})
}

func TestUploadPart(t *testing.T) {
	ctx := context.Background()

	t.Run("uploads part successfully", func(t *testing.T) {
		tmpDir := t.TempDir()
		a, err := New(tmpDir)
		if err != nil {
			t.Fatalf("failed to create adapter: %v", err)
		}

		uploadID, err := a.InitiateUpload(ctx, "test.txt")
		if err != nil {
			t.Fatalf("failed to initiate upload: %v", err)
		}
		defer a.AbortUpload(ctx, uploadID)

		err = a.UploadPart(ctx, uploadID, 1, []byte("hello"))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("fails with invalid upload ID", func(t *testing.T) {
		tmpDir := t.TempDir()
		a, err := New(tmpDir)
		if err != nil {
			t.Fatalf("failed to create adapter: %v", err)
		}

		err = a.UploadPart(ctx, "invalid-id", 1, []byte("hello"))
		if err == nil {
			t.Fatal("expected error for invalid upload ID")
		}
	})

	t.Run("fails with invalid part number", func(t *testing.T) {
		tmpDir := t.TempDir()
		a, err := New(tmpDir)
		if err != nil {
			t.Fatalf("failed to create adapter: %v", err)
		}

		uploadID, err := a.InitiateUpload(ctx, "test.txt")
		if err != nil {
			t.Fatalf("failed to initiate upload: %v", err)
		}
		defer a.AbortUpload(ctx, uploadID)

		err = a.UploadPart(ctx, uploadID, 0, []byte("hello"))
		if err == nil {
			t.Fatal("expected error for part number 0")
		}

		err = a.UploadPart(ctx, uploadID, -1, []byte("hello"))
		if err == nil {
			t.Fatal("expected error for negative part number")
		}
	})

	t.Run("allows out of order uploads", func(t *testing.T) {
		tmpDir := t.TempDir()
		a, err := New(tmpDir)
		if err != nil {
			t.Fatalf("failed to create adapter: %v", err)
		}

		uploadID, err := a.InitiateUpload(ctx, "test.txt")
		if err != nil {
			t.Fatalf("failed to initiate upload: %v", err)
		}
		defer a.AbortUpload(ctx, uploadID)

		// Upload parts out of order
		if err := a.UploadPart(ctx, uploadID, 3, []byte("world")); err != nil {
			t.Fatalf("failed to upload part 3: %v", err)
		}
		if err := a.UploadPart(ctx, uploadID, 1, []byte("hello")); err != nil {
			t.Fatalf("failed to upload part 1: %v", err)
		}
		if err := a.UploadPart(ctx, uploadID, 2, []byte(" ")); err != nil {
			t.Fatalf("failed to upload part 2: %v", err)
		}
	})

	t.Run("respects context cancellation", func(t *testing.T) {
		tmpDir := t.TempDir()
		a, err := New(tmpDir)
		if err != nil {
			t.Fatalf("failed to create adapter: %v", err)
		}

		uploadID, err := a.InitiateUpload(ctx, "test.txt")
		if err != nil {
			t.Fatalf("failed to initiate upload: %v", err)
		}
		defer a.AbortUpload(context.Background(), uploadID)

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		err = a.UploadPart(ctx, uploadID, 1, []byte("hello"))
		if err != context.Canceled {
			t.Errorf("expected context.Canceled, got: %v", err)
		}
	})
}

func TestCompleteUpload(t *testing.T) {
	ctx := context.Background()

	t.Run("concatenates parts in order", func(t *testing.T) {
		tmpDir := t.TempDir()
		a, err := New(tmpDir)
		if err != nil {
			t.Fatalf("failed to create adapter: %v", err)
		}

		uploadID, err := a.InitiateUpload(ctx, "test.txt")
		if err != nil {
			t.Fatalf("failed to initiate upload: %v", err)
		}

		// Upload parts out of order
		if err := a.UploadPart(ctx, uploadID, 2, []byte(" ")); err != nil {
			t.Fatalf("failed to upload part 2: %v", err)
		}
		if err := a.UploadPart(ctx, uploadID, 1, []byte("hello")); err != nil {
			t.Fatalf("failed to upload part 1: %v", err)
		}
		if err := a.UploadPart(ctx, uploadID, 3, []byte("world")); err != nil {
			t.Fatalf("failed to upload part 3: %v", err)
		}

		// Complete upload
		if err := a.CompleteUpload(ctx, uploadID); err != nil {
			t.Fatalf("failed to complete upload: %v", err)
		}

		// Verify file content
		reader, err := a.Read(ctx, "test.txt")
		if err != nil {
			t.Fatalf("failed to read file: %v", err)
		}
		defer reader.Close()

		content, err := io.ReadAll(reader)
		if err != nil {
			t.Fatalf("failed to read content: %v", err)
		}

		if string(content) != "hello world" {
			t.Errorf("expected 'hello world', got '%s'", string(content))
		}
	})

	t.Run("creates parent directories", func(t *testing.T) {
		tmpDir := t.TempDir()
		a, err := New(tmpDir)
		if err != nil {
			t.Fatalf("failed to create adapter: %v", err)
		}

		uploadID, err := a.InitiateUpload(ctx, "a/b/c/test.txt")
		if err != nil {
			t.Fatalf("failed to initiate upload: %v", err)
		}

		if err := a.UploadPart(ctx, uploadID, 1, []byte("content")); err != nil {
			t.Fatalf("failed to upload part: %v", err)
		}

		if err := a.CompleteUpload(ctx, uploadID); err != nil {
			t.Fatalf("failed to complete upload: %v", err)
		}

		// Verify directories exist
		for _, dir := range []string{"a", "a/b", "a/b/c"} {
			exists, err := a.DirExists(ctx, dir)
			if err != nil {
				t.Fatalf("failed to check dir: %v", err)
			}
			if !exists {
				t.Errorf("expected directory '%s' to exist", dir)
			}
		}

		// Verify file exists
		exists, err := a.FileExists(ctx, "a/b/c/test.txt")
		if err != nil {
			t.Fatalf("failed to check file: %v", err)
		}
		if !exists {
			t.Error("expected file to exist")
		}
	})

	t.Run("fails with invalid upload ID", func(t *testing.T) {
		tmpDir := t.TempDir()
		a, err := New(tmpDir)
		if err != nil {
			t.Fatalf("failed to create adapter: %v", err)
		}

		err = a.CompleteUpload(ctx, "invalid-id")
		if err == nil {
			t.Fatal("expected error for invalid upload ID")
		}
	})

	t.Run("fails with no parts uploaded", func(t *testing.T) {
		tmpDir := t.TempDir()
		a, err := New(tmpDir)
		if err != nil {
			t.Fatalf("failed to create adapter: %v", err)
		}

		uploadID, err := a.InitiateUpload(ctx, "test.txt")
		if err != nil {
			t.Fatalf("failed to initiate upload: %v", err)
		}

		err = a.CompleteUpload(ctx, uploadID)
		if err == nil {
			t.Fatal("expected error when no parts uploaded")
		}
	})

	t.Run("cleans up temp directory after completion", func(t *testing.T) {
		tmpDir := t.TempDir()
		a, err := New(tmpDir)
		if err != nil {
			t.Fatalf("failed to create adapter: %v", err)
		}

		uploadID, err := a.InitiateUpload(ctx, "test.txt")
		if err != nil {
			t.Fatalf("failed to initiate upload: %v", err)
		}

		if err := a.UploadPart(ctx, uploadID, 1, []byte("content")); err != nil {
			t.Fatalf("failed to upload part: %v", err)
		}

		// Get the parts directory before completing
		uploadRegistry.RLock()
		info := uploadRegistry.uploads[uploadID]
		partsDir := info.partsDir
		uploadRegistry.RUnlock()

		if err := a.CompleteUpload(ctx, uploadID); err != nil {
			t.Fatalf("failed to complete upload: %v", err)
		}

		// Verify temp directory is cleaned up
		if _, err := os.Stat(partsDir); !os.IsNotExist(err) {
			t.Error("expected temp directory to be cleaned up")
		}
	})

	t.Run("respects context cancellation", func(t *testing.T) {
		tmpDir := t.TempDir()
		a, err := New(tmpDir)
		if err != nil {
			t.Fatalf("failed to create adapter: %v", err)
		}

		uploadID, err := a.InitiateUpload(ctx, "test.txt")
		if err != nil {
			t.Fatalf("failed to initiate upload: %v", err)
		}

		if err := a.UploadPart(ctx, uploadID, 1, []byte("content")); err != nil {
			t.Fatalf("failed to upload part: %v", err)
		}

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		err = a.CompleteUpload(ctx, uploadID)
		if err != context.Canceled {
			t.Errorf("expected context.Canceled, got: %v", err)
		}

		// Cleanup
		_ = a.AbortUpload(context.Background(), uploadID)
	})
}

func TestAbortUpload(t *testing.T) {
	ctx := context.Background()

	t.Run("cleans up temp directory", func(t *testing.T) {
		tmpDir := t.TempDir()
		a, err := New(tmpDir)
		if err != nil {
			t.Fatalf("failed to create adapter: %v", err)
		}

		uploadID, err := a.InitiateUpload(ctx, "test.txt")
		if err != nil {
			t.Fatalf("failed to initiate upload: %v", err)
		}

		// Upload some parts
		if err := a.UploadPart(ctx, uploadID, 1, []byte("hello")); err != nil {
			t.Fatalf("failed to upload part: %v", err)
		}

		// Get the parts directory before aborting
		uploadRegistry.RLock()
		info := uploadRegistry.uploads[uploadID]
		partsDir := info.partsDir
		uploadRegistry.RUnlock()

		// Abort upload
		if err := a.AbortUpload(ctx, uploadID); err != nil {
			t.Fatalf("failed to abort upload: %v", err)
		}

		// Verify temp directory is cleaned up
		if _, err := os.Stat(partsDir); !os.IsNotExist(err) {
			t.Error("expected temp directory to be cleaned up")
		}
	})

	t.Run("fails with invalid upload ID", func(t *testing.T) {
		tmpDir := t.TempDir()
		a, err := New(tmpDir)
		if err != nil {
			t.Fatalf("failed to create adapter: %v", err)
		}

		err = a.AbortUpload(ctx, "invalid-id")
		if err == nil {
			t.Fatal("expected error for invalid upload ID")
		}
	})

	t.Run("respects context cancellation", func(t *testing.T) {
		tmpDir := t.TempDir()
		a, err := New(tmpDir)
		if err != nil {
			t.Fatalf("failed to create adapter: %v", err)
		}

		uploadID, err := a.InitiateUpload(ctx, "test.txt")
		if err != nil {
			t.Fatalf("failed to initiate upload: %v", err)
		}

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		err = a.AbortUpload(ctx, uploadID)
		if err != context.Canceled {
			t.Errorf("expected context.Canceled, got: %v", err)
		}

		// Cleanup with fresh context
		_ = a.AbortUpload(context.Background(), uploadID)
	})
}

func TestChunkedUploadLargeFile(t *testing.T) {
	ctx := context.Background()

	t.Run("handles large file with multiple chunks", func(t *testing.T) {
		tmpDir := t.TempDir()
		a, err := New(tmpDir)
		if err != nil {
			t.Fatalf("failed to create adapter: %v", err)
		}

		uploadID, err := a.InitiateUpload(ctx, "large.bin")
		if err != nil {
			t.Fatalf("failed to initiate upload: %v", err)
		}

		// Create large content (10 chunks of 1KB each = 10KB total)
		chunkSize := 1024
		numChunks := 10
		totalSize := chunkSize * numChunks

		expectedContent := make([]byte, 0, totalSize)
		for i := 1; i <= numChunks; i++ {
			chunk := bytes.Repeat([]byte{byte(i)}, chunkSize)
			expectedContent = append(expectedContent, chunk...)

			if err := a.UploadPart(ctx, uploadID, i, chunk); err != nil {
				t.Fatalf("failed to upload part %d: %v", i, err)
			}
		}

		if err := a.CompleteUpload(ctx, uploadID); err != nil {
			t.Fatalf("failed to complete upload: %v", err)
		}

		// Verify content
		reader, err := a.Read(ctx, "large.bin")
		if err != nil {
			t.Fatalf("failed to read file: %v", err)
		}
		defer reader.Close()

		content, err := io.ReadAll(reader)
		if err != nil {
			t.Fatalf("failed to read content: %v", err)
		}

		if !bytes.Equal(content, expectedContent) {
			t.Error("content mismatch")
		}
	})
}

func TestChunkedUploadIntegration(t *testing.T) {
	ctx := context.Background()

	t.Run("works with filekit.Upload helper", func(t *testing.T) {
		tmpDir := t.TempDir()
		a, err := New(tmpDir)
		if err != nil {
			t.Fatalf("failed to create adapter: %v", err)
		}

		content := bytes.Repeat([]byte("hello world "), 1000)
		reader := bytes.NewReader(content)

		err = filekit.Upload(ctx, a, "uploaded.txt", reader, int64(len(content)), &filekit.UploadOptions{
			ChunkSize: 1024, // 1KB chunks
		})
		if err != nil {
			t.Fatalf("upload failed: %v", err)
		}

		// Verify file exists and content matches
		exists, err := a.FileExists(ctx, "uploaded.txt")
		if err != nil {
			t.Fatalf("failed to check file: %v", err)
		}
		if !exists {
			t.Fatal("expected file to exist")
		}

		readContent, err := a.ReadAll(ctx, "uploaded.txt")
		if err != nil {
			t.Fatalf("failed to read file: %v", err)
		}

		if !bytes.Equal(readContent, content) {
			t.Error("content mismatch")
		}
	})
}

func TestChunkedUploaderInterface(t *testing.T) {
	// Verify Adapter implements ChunkedUploader
	var _ filekit.ChunkedUploader = (*Adapter)(nil)
}

func TestConcurrentChunkedUploads(t *testing.T) {
	ctx := context.Background()

	t.Run("handles multiple concurrent uploads", func(t *testing.T) {
		tmpDir := t.TempDir()
		a, err := New(tmpDir)
		if err != nil {
			t.Fatalf("failed to create adapter: %v", err)
		}

		numUploads := 5
		done := make(chan error, numUploads)

		for i := 0; i < numUploads; i++ {
			go func(n int) {
				path := filepath.Join("concurrent", "file"+string(rune('0'+n))+".txt")
				content := bytes.Repeat([]byte(string(rune('a'+n))), 100)

				uploadID, err := a.InitiateUpload(ctx, path)
				if err != nil {
					done <- err
					return
				}

				// Upload in 2 parts
				if err := a.UploadPart(ctx, uploadID, 1, content[:50]); err != nil {
					done <- err
					return
				}
				if err := a.UploadPart(ctx, uploadID, 2, content[50:]); err != nil {
					done <- err
					return
				}

				if err := a.CompleteUpload(ctx, uploadID); err != nil {
					done <- err
					return
				}

				done <- nil
			}(i)
		}

		// Wait for all uploads
		for i := 0; i < numUploads; i++ {
			if err := <-done; err != nil {
				t.Errorf("upload failed: %v", err)
			}
		}

		// Verify all files exist
		for i := 0; i < numUploads; i++ {
			path := filepath.Join("concurrent", "file"+string(rune('0'+i))+".txt")
			exists, err := a.FileExists(ctx, path)
			if err != nil {
				t.Errorf("failed to check file %s: %v", path, err)
			}
			if !exists {
				t.Errorf("expected file %s to exist", path)
			}
		}
	})
}
