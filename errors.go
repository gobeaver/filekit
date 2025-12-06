package filekit

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"strings"
	"time"
)

// ============================================================================
// ERROR CODES (19 total - Stable API, NEVER change values, only add)
// ============================================================================

// ErrorCode is a stable identifier for error types.
// Part of the public API contract - values will NEVER change.
type ErrorCode string

const (
	// Existence
	ErrCodeNotFound      ErrorCode = "FILEKIT_NOT_FOUND"
	ErrCodeAlreadyExists ErrorCode = "FILEKIT_ALREADY_EXISTS"
	ErrCodeTypeMismatch  ErrorCode = "FILEKIT_TYPE_MISMATCH"

	// Access
	ErrCodePermission ErrorCode = "FILEKIT_PERMISSION"
	ErrCodeAuth       ErrorCode = "FILEKIT_AUTH"
	ErrCodeQuota      ErrorCode = "FILEKIT_QUOTA"

	// Validation (use Details map for specifics)
	ErrCodeInvalidInput ErrorCode = "FILEKIT_INVALID_INPUT"
	ErrCodeValidation   ErrorCode = "FILEKIT_VALIDATION"

	// Integrity (cryptographic/data integrity failures)
	ErrCodeIntegrity ErrorCode = "FILEKIT_INTEGRITY"

	// Operation
	ErrCodeNotSupported ErrorCode = "FILEKIT_NOT_SUPPORTED"
	ErrCodeAborted      ErrorCode = "FILEKIT_ABORTED"
	ErrCodeTimeout      ErrorCode = "FILEKIT_TIMEOUT"
	ErrCodeClosed       ErrorCode = "FILEKIT_CLOSED"

	// Infrastructure
	ErrCodeIO        ErrorCode = "FILEKIT_IO"
	ErrCodeNetwork   ErrorCode = "FILEKIT_NETWORK"
	ErrCodeService   ErrorCode = "FILEKIT_SERVICE"
	ErrCodeRateLimit ErrorCode = "FILEKIT_RATE_LIMIT"

	// Mount (use Details for: not_found, exists, cross_mount)
	ErrCodeMount ErrorCode = "FILEKIT_MOUNT"

	// Internal
	ErrCodeInternal ErrorCode = "FILEKIT_INTERNAL"
)

func (c ErrorCode) String() string { return string(c) }

// ============================================================================
// ERROR CATEGORIES
// ============================================================================

type ErrorCategory int

const (
	CategoryUnknown      ErrorCategory = iota
	CategoryNotFound                   // Resource missing
	CategoryConflict                   // Already exists
	CategoryPermission                 // Access denied
	CategoryValidation                 // Bad input
	CategoryTransient                  // Retry may help
	CategoryPermanent                  // Don't retry
	CategoryNotSupported               // Feature unavailable
)

func (c ErrorCategory) String() string {
	names := [...]string{"unknown", "not_found", "conflict", "permission", "validation", "transient", "permanent", "not_supported"}
	if int(c) < len(names) {
		return names[c]
	}
	return "unknown"
}

func (c ErrorCategory) IsRetryable() bool { return c == CategoryTransient }

func codeToCategory(code ErrorCode) ErrorCategory {
	switch code {
	case ErrCodeNotFound:
		return CategoryNotFound
	case ErrCodeAlreadyExists:
		return CategoryConflict
	case ErrCodeTypeMismatch, ErrCodeInvalidInput, ErrCodeValidation:
		return CategoryValidation
	case ErrCodePermission, ErrCodeAuth, ErrCodeQuota:
		return CategoryPermission
	case ErrCodeNotSupported:
		return CategoryNotSupported
	case ErrCodeNetwork, ErrCodeRateLimit:
		return CategoryTransient
	// ErrCodeIntegrity and ErrCodeMount fall through to CategoryPermanent:
	// - Integrity: crypto/data corruption won't be fixed by retrying
	// - Mount: can be not_found, already_exists, or cross_mount - use Details() for specifics
	default:
		return CategoryPermanent
	}
}

// ============================================================================
// INTERFACES
// ============================================================================

type Coder interface {
	error
	Code() ErrorCode
}

type Categorizer interface {
	error
	Category() ErrorCategory
}

type Retryable interface {
	error
	IsRetryable() bool
	RetryAfter() time.Duration
}

type HTTPError interface {
	error
	HTTPStatus() int
}

type DetailedError interface {
	error
	Details() map[string]any
}

// ============================================================================
// FILE ERROR (Primary Error Type)
// ============================================================================

type FileError struct {
	ErrCode    ErrorCode      `json:"code"`
	Message    string         `json:"message"`
	Cat        ErrorCategory  `json:"category"`
	Op         string         `json:"op,omitempty"`
	Path       string         `json:"path,omitempty"`
	Driver     string         `json:"driver,omitempty"`
	Err        error          `json:"-"`
	Retry      bool           `json:"retryable"`
	RetryDelay time.Duration  `json:"retry_after,omitempty"`
	Detail     map[string]any `json:"details,omitempty"`
	Timestamp  time.Time      `json:"timestamp"`
	RequestID  string         `json:"request_id,omitempty"`
}

// error interface
func (e *FileError) Error() string {
	var b strings.Builder
	if e.Op != "" {
		b.WriteString(e.Op)
		b.WriteString(" ")
	}
	if e.Path != "" {
		b.WriteString(e.Path)
		b.WriteString(": ")
	}
	b.WriteString("[")
	b.WriteString(string(e.ErrCode))
	b.WriteString("] ")
	b.WriteString(e.Message)
	return b.String()
}

// errors.Unwrap support
func (e *FileError) Unwrap() error { return e.Err }

// errors.Is support - CRITICAL for stdlib compatibility
func (e *FileError) Is(target error) bool {
	if fe, ok := target.(*FileError); ok {
		return e.ErrCode == fe.ErrCode
	}
	switch e.ErrCode {
	case ErrCodeNotFound:
		return target == fs.ErrNotExist || target == os.ErrNotExist
	case ErrCodeAlreadyExists:
		return target == fs.ErrExist || target == os.ErrExist
	case ErrCodePermission:
		return target == fs.ErrPermission || target == os.ErrPermission
	case ErrCodeClosed:
		return target == fs.ErrClosed || target == os.ErrClosed
	case ErrCodeInvalidInput:
		return target == fs.ErrInvalid || target == os.ErrInvalid
	}
	return false
}

// Interface implementations
func (e *FileError) Code() ErrorCode           { return e.ErrCode }
func (e *FileError) Category() ErrorCategory   { return e.Cat }
func (e *FileError) IsRetryable() bool         { return e.Retry }
func (e *FileError) RetryAfter() time.Duration { return e.RetryDelay }
func (e *FileError) Details() map[string]any   { return e.Detail }

func (e *FileError) HTTPStatus() int {
	switch e.ErrCode {
	case ErrCodeNotFound:
		return http.StatusNotFound
	case ErrCodeAlreadyExists:
		return http.StatusConflict
	case ErrCodePermission:
		return http.StatusForbidden
	case ErrCodeAuth:
		return http.StatusUnauthorized
	case ErrCodeQuota:
		return http.StatusInsufficientStorage
	case ErrCodeInvalidInput, ErrCodeValidation, ErrCodeTypeMismatch:
		return http.StatusBadRequest
	case ErrCodeIntegrity:
		return http.StatusUnprocessableEntity
	case ErrCodeNotSupported:
		return http.StatusNotImplemented
	case ErrCodeTimeout:
		return http.StatusGatewayTimeout
	case ErrCodeRateLimit:
		return http.StatusTooManyRequests
	case ErrCodeAborted:
		return http.StatusRequestTimeout
	case ErrCodeService:
		return http.StatusBadGateway
	default:
		return http.StatusInternalServerError
	}
}

// Fluent builders
func (e *FileError) WithCause(err error) *FileError     { e.Err = err; return e }
func (e *FileError) WithDriver(d string) *FileError     { e.Driver = d; return e }
func (e *FileError) WithRequestID(id string) *FileError { e.RequestID = id; return e }
func (e *FileError) WithRetry(r bool, d time.Duration) *FileError {
	e.Retry = r
	e.RetryDelay = d
	return e
}
func (e *FileError) WithDetail(k string, v any) *FileError {
	if e.Detail == nil {
		e.Detail = make(map[string]any)
	}
	e.Detail[k] = v
	return e
}

// ============================================================================
// CONSTRUCTORS
// ============================================================================

func NewError(code ErrorCode, message string) *FileError {
	cat := codeToCategory(code)
	return &FileError{
		ErrCode:   code,
		Message:   message,
		Cat:       cat,
		Retry:     cat.IsRetryable(),
		Timestamp: time.Now().UTC(),
	}
}

func NewPathError(op, path string, code ErrorCode, message string) *FileError {
	e := NewError(code, message)
	e.Op = op
	e.Path = path
	return e
}

func Wrap(err error, code ErrorCode, message string) *FileError {
	e := NewError(code, message)
	e.Err = err
	return e
}

func WrapPath(err error, op, path string, code ErrorCode, message string) *FileError {
	e := NewPathError(op, path, code, message)
	e.Err = err
	return e
}

// WrapPathErr wraps an error with path context, auto-inferring the error code.
// This is a convenience function for backward compatibility with code that
// doesn't want to specify an explicit error code.
func WrapPathErr(op, path string, err error) *FileError {
	code := inferErrorCode(err)
	return WrapPath(err, op, path, code, err.Error())
}

// inferErrorCode tries to determine the appropriate error code from the error.
func inferErrorCode(err error) ErrorCode {
	if err == nil {
		return ErrCodeInternal
	}
	switch {
	case errors.Is(err, os.ErrNotExist), errors.Is(err, fs.ErrNotExist):
		return ErrCodeNotFound
	case errors.Is(err, os.ErrExist), errors.Is(err, fs.ErrExist):
		return ErrCodeAlreadyExists
	case errors.Is(err, os.ErrPermission), errors.Is(err, fs.ErrPermission):
		return ErrCodePermission
	case errors.Is(err, os.ErrClosed), errors.Is(err, fs.ErrClosed):
		return ErrCodeClosed
	case errors.Is(err, os.ErrInvalid), errors.Is(err, fs.ErrInvalid):
		return ErrCodeInvalidInput
	case errors.Is(err, context.Canceled):
		return ErrCodeAborted
	case errors.Is(err, context.DeadlineExceeded):
		return ErrCodeTimeout
	case errors.Is(err, ErrNotSupported):
		return ErrCodeNotSupported
	case errors.Is(err, ErrNotAllowed):
		return ErrCodePermission
	case errors.Is(err, ErrInvalidOffset), errors.Is(err, ErrInvalidWhence), errors.Is(err, ErrInvalidName), errors.Is(err, ErrInvalidSize):
		return ErrCodeInvalidInput
	case errors.Is(err, ErrNoSpace):
		return ErrCodeQuota
	default:
		return ErrCodeInternal
	}
}

func FromContext(ctx context.Context, op, path string) *FileError {
	if ctx.Err() == nil {
		return nil
	}
	if errors.Is(ctx.Err(), context.Canceled) {
		return NewPathError(op, path, ErrCodeAborted, "operation canceled")
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return NewPathError(op, path, ErrCodeTimeout, "deadline exceeded")
	}
	return WrapPath(ctx.Err(), op, path, ErrCodeAborted, "context error")
}

// ============================================================================
// HELPER FUNCTIONS
// ============================================================================

func IsCode(err error, code ErrorCode) bool {
	var fe *FileError
	if errors.As(err, &fe) {
		return fe.ErrCode == code
	}
	return false
}

func GetCode(err error) ErrorCode {
	var fe *FileError
	if errors.As(err, &fe) {
		return fe.ErrCode
	}
	return ""
}

func GetCategory(err error) ErrorCategory {
	var fe *FileError
	if errors.As(err, &fe) {
		return fe.Cat
	}
	return CategoryUnknown
}

func IsRetryableErr(err error) bool {
	var r Retryable
	if errors.As(err, &r) {
		return r.IsRetryable()
	}
	return false
}

func GetRetryAfter(err error) time.Duration {
	var r Retryable
	if errors.As(err, &r) {
		return r.RetryAfter()
	}
	return 0
}

// Semantic helpers
func IsNotFound(err error) bool {
	return errors.Is(err, fs.ErrNotExist) || IsCode(err, ErrCodeNotFound)
}

func IsPermissionErr(err error) bool {
	return errors.Is(err, fs.ErrPermission) || IsCode(err, ErrCodePermission) || IsCode(err, ErrCodeAuth)
}

func IsValidationErr(err error) bool { return GetCategory(err) == CategoryValidation }
func IsTemporary(err error) bool     { return GetCategory(err) == CategoryTransient || IsRetryableErr(err) }

// Convert any error to FileError
func ToFileError(err error) *FileError {
	if err == nil {
		return nil
	}
	var fe *FileError
	if errors.As(err, &fe) {
		return fe
	}
	switch {
	case errors.Is(err, os.ErrNotExist), errors.Is(err, fs.ErrNotExist):
		return Wrap(err, ErrCodeNotFound, "not found")
	case errors.Is(err, os.ErrExist), errors.Is(err, fs.ErrExist):
		return Wrap(err, ErrCodeAlreadyExists, "already exists")
	case errors.Is(err, os.ErrPermission), errors.Is(err, fs.ErrPermission):
		return Wrap(err, ErrCodePermission, "permission denied")
	case errors.Is(err, os.ErrClosed), errors.Is(err, fs.ErrClosed):
		return Wrap(err, ErrCodeClosed, "already closed")
	case errors.Is(err, context.Canceled):
		return Wrap(err, ErrCodeAborted, "canceled")
	case errors.Is(err, context.DeadlineExceeded):
		return Wrap(err, ErrCodeTimeout, "deadline exceeded")
	}
	return Wrap(err, ErrCodeInternal, err.Error())
}

// ============================================================================
// MULTI ERROR (Batch Operations)
// ============================================================================

type MultiError struct {
	Op     string
	Path   string
	Errors []error
	Total  int
}

func NewMultiError(op string) *MultiError { return &MultiError{Op: op} }

func (m *MultiError) Add(err error) {
	if err != nil {
		m.Errors = append(m.Errors, err)
	}
	m.Total++
}

func (m *MultiError) Err() error {
	switch len(m.Errors) {
	case 0:
		return nil
	case 1:
		return m.Errors[0]
	default:
		return m
	}
}

func (m *MultiError) Error() string {
	return fmt.Sprintf("%s: %d/%d operations failed", m.Op, len(m.Errors), m.Total)
}

func (m *MultiError) Unwrap() []error      { return m.Errors }
func (m *MultiError) HasErrors() bool      { return len(m.Errors) > 0 }
func (m *MultiError) PartialSuccess() bool { return len(m.Errors) > 0 && len(m.Errors) < m.Total }

// ============================================================================
// BACKWARD COMPATIBILITY (Deprecated - kept for existing code)
// ============================================================================

var (
	ErrNotExist   = fs.ErrNotExist
	ErrExist      = fs.ErrExist
	ErrPermission = fs.ErrPermission
	ErrClosed     = fs.ErrClosed
	ErrInvalid    = fs.ErrInvalid

	// Deprecated: Use NewError with appropriate code
	ErrNotSupported     = errors.New("operation not supported")
	ErrReadOnly         = errors.New("filesystem is read-only")
	ErrInvalidSize      = errors.New("invalid file size")
	ErrNoSpace          = errors.New("no space left on device")
	ErrNotDir           = errors.New("not a directory")
	ErrIsDir            = errors.New("is a directory")
	ErrNotEmpty         = errors.New("directory not empty")
	ErrInvalidName      = errors.New("invalid name")
	ErrNotAllowed       = errors.New("operation not allowed")
	ErrInvalidOffset    = errors.New("invalid offset")
	ErrInvalidWhence    = errors.New("invalid whence")
	ErrMountNotFound    = errors.New("no mount point found for path")
	ErrMountExists      = errors.New("mount point already exists")
	ErrInvalidMountPath = errors.New("invalid mount path")
	ErrEmptyMountPath   = errors.New("mount path cannot be empty")
	ErrNilDriver        = errors.New("driver cannot be nil")
	ErrCrossMount       = errors.New("operation cannot cross mount boundaries")
)

// Deprecated: Use IsNotFound
func IsNotExist(err error) bool { return IsNotFound(err) }

// Deprecated: Use IsPermissionErr
func IsPermission(err error) bool { return IsPermissionErr(err) }

// Deprecated: Use errors.Is(err, fs.ErrExist)
func IsExist(err error) bool { return errors.Is(err, fs.ErrExist) || IsCode(err, ErrCodeAlreadyExists) }

// ============================================================================
// INTERFACE ASSERTIONS
// ============================================================================

var (
	_ error         = (*FileError)(nil)
	_ Coder         = (*FileError)(nil)
	_ Categorizer   = (*FileError)(nil)
	_ Retryable     = (*FileError)(nil)
	_ HTTPError     = (*FileError)(nil)
	_ DetailedError = (*FileError)(nil)
)
