// Package filevalidator provides high-performance, memory-efficient file validation
// for Go applications. It validates file types, sizes, and content without loading
// entire files into memory.
//
// FileValidator is part of [FileKit] but can be used as a standalone package with
// zero external dependencies.
//
// [FileKit]: https://github.com/gobeaver/filekit
//
// # Features
//
//   - Memory efficient: header-only validation (500MB file uses ~2KB RAM)
//   - 60+ file formats supported (images, documents, archives, audio, video, text)
//   - Zero dependencies: pure Go, no CGO
//   - Fluent builder API for clean, chainable configuration
//   - Content validation: zip bomb protection, path traversal detection, XXE protection
//   - Magic bytes detection: accurate MIME detection beyond http.DetectContentType
//
// # Quick Start
//
// Using presets:
//
//	// Validate images (jpg, png, gif, webp, svg, etc.) up to 10MB
//	validator := filevalidator.ForImages().Build()
//	err := validator.Validate(fileHeader)
//
// Using the builder API:
//
//	validator := filevalidator.NewBuilder().
//	    MaxSize(10 * filevalidator.MB).
//	    Accept("image/*").
//	    Extensions(".jpg", ".png", ".webp").
//	    WithContentValidation().
//	    Build()
//
//	err := validator.Validate(fileHeader)
//
// # Validation Methods
//
// FileValidator provides multiple validation entry points:
//
//	// From multipart.FileHeader (HTTP uploads)
//	err := validator.Validate(header)
//
//	// With context (cancellable)
//	err := validator.ValidateWithContext(ctx, header)
//
//	// From io.Reader
//	err := validator.ValidateReader(reader, "file.jpg", size)
//
//	// From bytes
//	err := validator.ValidateBytes(data, "file.jpg")
//
//	// Local file
//	err := filevalidator.ValidateLocalFile(validator, "/path/to/file.jpg")
//
// # Presets
//
// Pre-configured validators for common use cases:
//
//	filevalidator.ForImages()     // JPEG, PNG, GIF, WebP, SVG, BMP, TIFF - 10MB max
//	filevalidator.ForDocuments()  // PDF, Word, Excel, PowerPoint, TXT, CSV - 50MB max
//	filevalidator.ForMedia()      // Audio + Video - 500MB max
//	filevalidator.ForArchives()   // ZIP, TAR, GZIP - 1GB max
//	filevalidator.ForWeb()        // Images + Documents - 25MB max
//	filevalidator.Strict()        // Strict MIME validation, required extension
//
// Presets can be customized:
//
//	validator := filevalidator.ForImages().
//	    MaxSize(5 * filevalidator.MB).     // Override default 10MB
//	    Extensions(".png", ".jpg").        // More restrictive
//	    Build()
//
// # Content Validators
//
// Content validators inspect file headers and structure to detect malicious files:
//
//   - Archives: ZIP bomb detection, path traversal, nested archive limits
//   - Images: Dimension limits, decompression bomb protection
//   - Office: ZIP structure validation, macro detection
//   - XML: XXE protection, depth limits
//   - PDF: Header/trailer structure validation
//
// All validators read only file headers, never loading full content into memory.
//
// # MIME Detection
//
// Enhanced MIME detection using magic bytes (60+ signatures):
//
//	// Detect MIME type from reader
//	mime, err := filevalidator.DetectMIME(reader)
//
//	// Detect from bytes (zero allocation)
//	mime := filevalidator.DetectMIMEFromBytes(data)
//
//	// Helper functions
//	filevalidator.IsBinaryMIME("image/png")              // true
//	filevalidator.IsExecutableMIME("application/x-msdownload")  // true
//	filevalidator.GetMIMECategory("video/mp4")           // "video"
//
// # Error Handling
//
// Validation errors include the error type for programmatic handling:
//
//	err := validator.Validate(header)
//	if err != nil {
//	    switch {
//	    case filevalidator.IsErrorOfType(err, filevalidator.ErrorTypeSize):
//	        // File too large or too small
//	    case filevalidator.IsErrorOfType(err, filevalidator.ErrorTypeMIME):
//	        // Invalid MIME type
//	    case filevalidator.IsErrorOfType(err, filevalidator.ErrorTypeExtension):
//	        // Blocked extension
//	    case filevalidator.IsErrorOfType(err, filevalidator.ErrorTypeContent):
//	        // Content validation failed (zip bomb, etc.)
//	    }
//	}
//
// # Custom Content Validators
//
// Register custom validators for proprietary formats:
//
//	type MyValidator struct{}
//
//	func (v *MyValidator) ValidateContent(reader io.Reader, size int64) error {
//	    // Read only what you need (e.g., first 512 bytes)
//	    header := make([]byte, 512)
//	    io.ReadFull(reader, header)
//	    // Validate...
//	    return nil
//	}
//
//	func (v *MyValidator) SupportedMIMETypes() []string {
//	    return []string{"application/x-custom"}
//	}
//
//	registry := filevalidator.DefaultRegistry()
//	registry.Register("application/x-custom", &MyValidator{})
//
// # FileKit Integration
//
// When used with FileKit, validation can be applied automatically on every write:
//
//	import (
//	    "github.com/gobeaver/filekit"
//	    "github.com/gobeaver/filekit/filevalidator"
//	    "github.com/gobeaver/filekit/driver/local"
//	)
//
//	fs, _ := local.New("/var/uploads")
//	validator := filevalidator.ForImages().MaxSize(5 * filevalidator.MB).Build()
//	validatedFS := filekit.NewValidatedFileSystem(fs, validator)
//
//	// All writes are now automatically validated
//	err := validatedFS.Write(ctx, "photo.jpg", reader)
//
// # Design Philosophy
//
// This package does type validation, not security scanning. It validates that files
// are what they claim to be and protects against common file-based attacks (zip bombs,
// path traversal, XXE). For malware detection, use dedicated tools like ClamAV.
//
// Typical validation pipeline:
//
//	1. Auth + Rate Limit     (instant)
//	2. Size + Extension      (instant)
//	3. Type Validation       (fast) ← filevalidator
//	4. Malware Scan          (slow) ← ClamAV or similar
//	5. Storage               (I/O)
package filevalidator
