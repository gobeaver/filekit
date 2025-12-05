package filekit_test

import (
	"context"
	"fmt"
	"strings"

	"github.com/gobeaver/filekit"
	"github.com/gobeaver/filekit/driver/memory"
)

func ExampleMountManager() {
	ctx := context.Background()

	// Create a mount manager
	mounts := filekit.NewMountManager()

	// Mount different backends under virtual paths
	localFS := memory.New() // Using memory for example; use local.New() in production
	cloudFS := memory.New() // Using memory for example; use s3.New() in production

	_ = mounts.Mount("/local", localFS)
	_ = mounts.Mount("/cloud", cloudFS)

	// Write to different mounts transparently
	_, _ = mounts.Write(ctx, "/local/file.txt", strings.NewReader("local content"))
	_, _ = mounts.Write(ctx, "/cloud/file.txt", strings.NewReader("cloud content"))

	// Read from mounts
	localData, _ := mounts.ReadAll(ctx, "/local/file.txt")
	cloudData, _ := mounts.ReadAll(ctx, "/cloud/file.txt")

	fmt.Println(string(localData))
	fmt.Println(string(cloudData))
	// Output:
	// local content
	// cloud content
}

func ExampleMountManager_crossMountCopy() {
	ctx := context.Background()

	mounts := filekit.NewMountManager()
	_ = mounts.Mount("/source", memory.New())
	_ = mounts.Mount("/dest", memory.New())

	// Write a file to source
	_, _ = mounts.Write(ctx, "/source/data.txt", strings.NewReader("important data"))

	// Copy across mounts (automatically handles read from source, write to dest)
	err := mounts.Copy(ctx, "/source/data.txt", "/dest/backup.txt")
	if err != nil {
		fmt.Println("Error:", err)
		return
	}

	// Verify the copy
	data, _ := mounts.ReadAll(ctx, "/dest/backup.txt")
	fmt.Println(string(data))
	// Output:
	// important data
}

func ExampleListWithSelector() {
	ctx := context.Background()
	fs := memory.New()

	// Create some test files
	_, _ = fs.Write(ctx, "doc.txt", strings.NewReader("text"))
	_, _ = fs.Write(ctx, "image.jpg", strings.NewReader("jpeg"))
	_, _ = fs.Write(ctx, "photo.jpg", strings.NewReader("jpeg"))
	_, _ = fs.Write(ctx, "data.json", strings.NewReader("json"))

	// List only .jpg files using glob selector
	files, _ := filekit.ListWithSelector(ctx, fs, "/", filekit.Glob("*.jpg"), false)

	for i := range files {
		fmt.Println(files[i].Name)
	}
	// Output:
	// image.jpg
	// photo.jpg
}

func ExampleAnd() {
	ctx := context.Background()
	fs := memory.New()

	// Create test files
	_, _ = fs.Write(ctx, "small.txt", strings.NewReader("hi"))
	_, _ = fs.Write(ctx, "large.txt", strings.NewReader(strings.Repeat("x", 1000)))
	_, _ = fs.Write(ctx, "small.jpg", strings.NewReader("img"))

	// Combine selectors: .txt files under 100 bytes
	selector := filekit.And(
		filekit.Glob("*.txt"),
		filekit.FuncSelector(func(f *filekit.FileInfo) bool {
			return f.Size < 100
		}),
	)

	files, _ := filekit.ListWithSelector(ctx, fs, "/", selector, false)

	for i := range files {
		fmt.Printf("%s (%d bytes)\n", files[i].Name, files[i].Size)
	}
	// Output:
	// small.txt (2 bytes)
}

func ExampleOr() {
	ctx := context.Background()
	fs := memory.New()

	// Create test files
	_, _ = fs.Write(ctx, "readme.txt", strings.NewReader("text"))
	_, _ = fs.Write(ctx, "config.json", strings.NewReader("json"))
	_, _ = fs.Write(ctx, "image.png", strings.NewReader("png"))

	// Match .txt OR .json files
	selector := filekit.Or(
		filekit.Glob("*.txt"),
		filekit.Glob("*.json"),
	)

	files, _ := filekit.ListWithSelector(ctx, fs, "/", selector, false)

	for i := range files {
		fmt.Println(files[i].Name)
	}
	// Output:
	// config.json
	// readme.txt
}

func ExampleNot() {
	ctx := context.Background()
	fs := memory.New()

	// Create test files
	_, _ = fs.Write(ctx, "keep.txt", strings.NewReader("keep"))
	_, _ = fs.Write(ctx, "temp.tmp", strings.NewReader("temp"))
	_, _ = fs.Write(ctx, "data.txt", strings.NewReader("data"))

	// Match all files EXCEPT .tmp files
	selector := filekit.And(
		filekit.All(),
		filekit.Not(filekit.Glob("*.tmp")),
	)

	files, _ := filekit.ListWithSelector(ctx, fs, "/", selector, false)

	for i := range files {
		fmt.Println(files[i].Name)
	}
	// Output:
	// data.txt
	// keep.txt
}

func ExampleFuncSelector() {
	ctx := context.Background()
	fs := memory.New()

	// Create test files
	_, _ = fs.Write(ctx, "report_2024.pdf", strings.NewReader("pdf content"))
	_, _ = fs.Write(ctx, "image.jpg", strings.NewReader("jpg"))
	_, _ = fs.Write(ctx, "report_2023.pdf", strings.NewReader("old report"))

	// Custom selector: files containing "report" in the name
	selector := filekit.FuncSelector(func(f *filekit.FileInfo) bool {
		return strings.Contains(f.Name, "report")
	})

	files, _ := filekit.ListWithSelector(ctx, fs, "/", selector, false)

	for i := range files {
		fmt.Println(files[i].Name)
	}
	// Output:
	// report_2023.pdf
	// report_2024.pdf
}

func ExampleIsNotExist() {
	ctx := context.Background()
	fs := memory.New()

	// Try to read a non-existent file
	_, err := fs.Read(ctx, "nonexistent.txt")

	if filekit.IsNotExist(err) {
		fmt.Println("File does not exist")
	}
	// Output:
	// File does not exist
}

func ExampleNewReadOnlyFileSystem() {
	ctx := context.Background()
	fs := memory.New()

	// Write some initial data
	_, _ = fs.Write(ctx, "config.json", strings.NewReader(`{"setting": "value"}`))

	// Wrap with read-only protection
	readOnly := filekit.NewReadOnlyFileSystem(fs)

	// Reading works
	data, _ := readOnly.ReadAll(ctx, "config.json")
	fmt.Println("Read:", string(data))

	// Writing is blocked
	_, err := readOnly.Write(ctx, "new.txt", strings.NewReader("data"))
	if filekit.IsReadOnlyError(err) {
		fmt.Println("Write blocked: filesystem is read-only")
	}
	// Output:
	// Read: {"setting": "value"}
	// Write blocked: filesystem is read-only
}

func ExampleCanChecksum() {
	ctx := context.Background()
	var fs filekit.FileSystem = memory.New()

	// Write a file
	_, _ = fs.Write(ctx, "data.txt", strings.NewReader("Hello, World!"))

	// Check if filesystem supports checksums
	if cs, ok := fs.(filekit.CanChecksum); ok {
		// Calculate SHA256
		hash, _ := cs.Checksum(ctx, "data.txt", filekit.ChecksumSHA256)
		fmt.Println("SHA256:", hash)

		// Calculate multiple checksums in one pass
		hashes, _ := cs.Checksums(ctx, "data.txt", []filekit.ChecksumAlgorithm{
			filekit.ChecksumMD5,
			filekit.ChecksumCRC32,
		})
		fmt.Println("MD5:", hashes[filekit.ChecksumMD5])
		fmt.Println("CRC32:", hashes[filekit.ChecksumCRC32])
	}
	// Output:
	// SHA256: dffd6021bb2bd5b0af676290809ec3a53191dd81c7f70a4b28688a362182986f
	// MD5: 65a8e27d8879283831b664bd8b7f0ad4
	// CRC32: ec4ac3d0
}

func ExampleNewCompositeChangeToken() {
	// Create multiple change tokens (typically from different sources)
	token1 := &filekit.CancelledChangeToken{} // Already changed
	token2 := filekit.NeverChangeToken{}      // Never changes

	// Combine them - composite is changed if ANY token is changed
	composite := filekit.NewCompositeChangeToken(token1, token2)

	fmt.Println("Has changed:", composite.HasChanged())
	fmt.Println("Active callbacks:", composite.ActiveChangeCallbacks())
	// Output:
	// Has changed: true
	// Active callbacks: false
}

func ExampleOnChange() {
	ctx := context.Background()
	var fs filekit.FileSystem = memory.New()

	// Write initial file
	_, _ = fs.Write(ctx, "config.json", strings.NewReader(`{"version": 1}`))

	changeCount := 0

	// Set up continuous watching with OnChange helper
	cancel := filekit.OnChange(
		func() (filekit.ChangeToken, error) {
			if watcher, ok := fs.(filekit.CanWatch); ok {
				return watcher.Watch(ctx, "config.json")
			}
			return filekit.NeverChangeToken{}, nil
		},
		func() {
			changeCount++
			fmt.Println("Config changed!")
		},
	)
	defer cancel()

	// Simulate a change
	_, _ = fs.Write(ctx, "config.json", strings.NewReader(`{"version": 2}`))

	// Note: In real usage, the callback would be invoked asynchronously
	fmt.Println("Watching started")
	// Output:
	// Watching started
}
