package filekit

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// Common filesystem errors
var (
	ErrNotExist      = errors.New("file does not exist")
	ErrExist         = errors.New("file already exists")
	ErrPermission    = errors.New("permission denied")
	ErrClosed        = errors.New("file already closed")
	ErrNotDir        = errors.New("not a directory")
	ErrIsDir         = errors.New("is a directory")
	ErrNotEmpty      = errors.New("directory not empty")
	ErrInvalidName   = errors.New("invalid name")
	ErrInvalidOffset = errors.New("invalid offset")
	ErrInvalidWhence = errors.New("invalid whence")
	ErrNotSupported  = errors.New("operation not supported")
	ErrNotAllowed    = errors.New("operation not allowed")
	ErrInvalidSize   = errors.New("invalid file size")
	ErrNoSpace       = errors.New("no space left on device")
)

// ErrorCode represents a structured error code for programmatic handling.
type ErrorCode string

const (
	// File/Directory Errors
	ErrCodeNotFound      ErrorCode = "NOT_FOUND"
	ErrCodeAlreadyExists ErrorCode = "ALREADY_EXISTS"
	ErrCodeNotEmpty      ErrorCode = "NOT_EMPTY"
	ErrCodeIsDirectory   ErrorCode = "IS_DIRECTORY"
	ErrCodeNotDirectory  ErrorCode = "NOT_DIRECTORY"

	// Permission Errors
	ErrCodePermissionDenied ErrorCode = "PERMISSION_DENIED"
	ErrCodeForbidden        ErrorCode = "FORBIDDEN"
	ErrCodeUnauthorized     ErrorCode = "UNAUTHORIZED"

	// Resource Errors
	ErrCodeQuotaExceeded ErrorCode = "QUOTA_EXCEEDED"
	ErrCodeNoSpace       ErrorCode = "NO_SPACE"
	ErrCodeTooLarge      ErrorCode = "TOO_LARGE"

	// Validation Errors
	ErrCodeInvalidArgument ErrorCode = "INVALID_ARGUMENT"
	ErrCodeInvalidPath     ErrorCode = "INVALID_PATH"
	ErrCodeInvalidName     ErrorCode = "INVALID_NAME"

	// State Errors
	ErrCodeClosed           ErrorCode = "CLOSED"
	ErrCodeCancelled        ErrorCode = "CANCELLED"
	ErrCodeDeadlineExceeded ErrorCode = "DEADLINE_EXCEEDED"

	// Network/Service Errors
	ErrCodeUnavailable  ErrorCode = "UNAVAILABLE"
	ErrCodeTimeout      ErrorCode = "TIMEOUT"
	ErrCodeNetworkError ErrorCode = "NETWORK_ERROR"

	// Data Errors
	ErrCodeDataCorrupted    ErrorCode = "DATA_CORRUPTED"
	ErrCodeChecksumMismatch ErrorCode = "CHECKSUM_MISMATCH"
	ErrCodeVersionConflict  ErrorCode = "VERSION_CONFLICT"

	// Feature Errors
	ErrCodeNotSupported   ErrorCode = "NOT_SUPPORTED"
	ErrCodeNotImplemented ErrorCode = "NOT_IMPLEMENTED"

	// Unknown
	ErrCodeInternal ErrorCode = "INTERNAL"
	ErrCodeUnknown  ErrorCode = "UNKNOWN"
)

// PathError records an error and the operation and file path that caused it.
// It implements the error interface and supports errors.Unwrap for error chaining.
type PathError struct {
	// Op is the operation that failed (e.g., "read", "write", "delete", "stat").
	Op string

	// Path is the file or directory path involved in the failed operation.
	Path string

	// Err is the underlying error that caused the operation to fail.
	Err error

	// Code is a structured error code for programmatic handling.
	Code ErrorCode

	// Retryable indicates if the operation can be retried.
	Retryable bool

	// HTTPCode suggests an appropriate HTTP status code.
	// 0 means no suggestion.
	HTTPCode int

	// Context contains additional error context.
	Context map[string]interface{}

	// Timestamp is when the error occurred.
	Timestamp time.Time
}

// Error implements the error interface.
func (e *PathError) Error() string {
	if e.Code != "" {
		return fmt.Sprintf("%s %s: [%s] %v", e.Op, e.Path, e.Code, e.Err)
	}
	return fmt.Sprintf("%s %s: %v", e.Op, e.Path, e.Err)
}

// Unwrap returns the underlying error.
func (e *PathError) Unwrap() error {
	return e.Err
}

// IsCode checks if the error has the given error code.
func (e *PathError) IsCode(code ErrorCode) bool {
	return e.Code == code
}

// IsRetryable returns true if the error is retryable.
func (e *PathError) IsRetryable() bool {
	return e.Retryable
}

// WithContext adds context to the error.
func (e *PathError) WithContext(key string, value interface{}) *PathError {
	if e.Context == nil {
		e.Context = make(map[string]interface{})
	}
	e.Context[key] = value
	return e
}

// NewPathError creates a new PathError with sensible defaults.
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

// inferErrorCode tries to determine the error code from the error.
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
	case errors.Is(err, ErrInvalidName):
		return ErrCodeInvalidName
	case errors.Is(err, ErrClosed):
		return ErrCodeClosed
	case errors.Is(err, context.Canceled):
		return ErrCodeCancelled
	case errors.Is(err, context.DeadlineExceeded):
		return ErrCodeDeadlineExceeded
	default:
		return ErrCodeUnknown
	}
}

// isRetryableError determines if an error is retryable.
func isRetryableError(err error) bool {
	if err == nil {
		return false
	}

	// Check for context errors (cancelled is not retryable)
	if errors.Is(err, context.Canceled) {
		return false
	}

	// Timeout/deadline is retryable
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}

	// Temporary network errors are typically retryable
	// This would need more sophisticated logic in production

	return false
}

// inferHTTPCode suggests an HTTP status code from the error.
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
	case errors.Is(err, ErrNoSpace):
		return 507
	case errors.Is(err, context.Canceled):
		return 499
	case errors.Is(err, context.DeadlineExceeded):
		return 504
	default:
		return 500
	}
}

// IsNotExist reports whether an error indicates that a file or directory
// does not exist.
func IsNotExist(err error) bool {
	var pathErr *PathError
	if errors.As(err, &pathErr) {
		return pathErr.Code == ErrCodeNotFound
	}
	return errors.Is(err, ErrNotExist)
}

// IsExist reports whether an error indicates that a file or directory
// already exists.
func IsExist(err error) bool {
	var pathErr *PathError
	if errors.As(err, &pathErr) {
		return pathErr.Code == ErrCodeAlreadyExists
	}
	return errors.Is(err, ErrExist)
}

// IsPermission reports whether an error indicates that permission is denied.
func IsPermission(err error) bool {
	var pathErr *PathError
	if errors.As(err, &pathErr) {
		return pathErr.Code == ErrCodePermissionDenied || pathErr.Code == ErrCodeForbidden
	}
	return errors.Is(err, ErrPermission)
}

// IsRetryable reports whether an error is retryable.
func IsRetryable(err error) bool {
	var pathErr *PathError
	if errors.As(err, &pathErr) {
		return pathErr.Retryable
	}
	return false
}

// GetErrorCode extracts the error code from an error.
func GetErrorCode(err error) ErrorCode {
	var pathErr *PathError
	if errors.As(err, &pathErr) {
		return pathErr.Code
	}
	return ErrCodeUnknown
}
