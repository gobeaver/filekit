# Changelog

All notable changes to FileKit will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Fixed

- **Breaking compatibility fix**: Updated all driver error handling to use the new error API introduced in v0.0.2
  - Drivers now use `WrapPathErr(op, path, err)` for wrapping external errors (auto-infers error code)
  - Drivers now use `NewPathError(op, path, code, message)` with explicit error codes for domain errors
  - Affected drivers: S3, Azure, SFTP, ZIP

#### Driver-specific fixes

**S3 Driver** (`driver/s3`)
- `Write`: Use `WrapPathErr` for `io.ReadAll` errors
- `WriteFile`: Use `WrapPathErr` for `os.Open` errors

**Azure Driver** (`driver/azure`)
- `Write`: Use `ErrCodeAlreadyExists` for file exists check, `WrapPathErr` for `io.ReadAll` errors
- `WriteFile`: Use `WrapPathErr` for `os.Open` errors

**SFTP Driver** (`driver/sftp`)
- `Write`: Use `ErrCodePermission` for path safety check, `ErrCodeAlreadyExists` for file exists check
- `Write`: Use `WrapPathErr` for connection, stat, mkdir, create, and copy errors
- `WriteFile`: Use `WrapPathErr` for `os.Open` errors

**ZIP Driver** (`driver/zip`)
- `Write`: Use `ErrCodePermission` for read-only mode and invalid path checks
- `Write`: Use `ErrCodeAlreadyExists` for file exists checks (both in files and pending maps)
- `Write`: Use `WrapPathErr` for `io.ReadAll`, `CreateHeader`, and `Write` errors
- `UploadFile`: Use `WrapPathErr` for `os.Open` errors

## [v0.0.2] - 2024-12-26

### Added

- LLM documentation (`llm.yaml`) for FileKit and FileValidator packages
- Extended FileInfo fields populated across all drivers
- Redesigned error handling system with stable codes and rich metadata
  - 19 stable error codes (`ErrCodeNotFound`, `ErrCodeAlreadyExists`, etc.)
  - Error categories for semantic grouping
  - `FileError` type implementing `Coder`, `Categorizer`, `Retryable`, `HTTPError`, `DetailedError`
  - `MultiError` for batch operations with partial success tracking
- S3 driver: stream uploads for seekable readers (`os.File`, `bytes.Reader`, etc.)

## [v0.0.1] - Initial Release

### Added

- Core filesystem abstraction with `FileReader`, `FileWriter`, `FileSystem` interfaces
- Optional capability interfaces: `CanCopy`, `CanMove`, `CanSignURL`, `CanChecksum`, `CanWatch`, `CanReadRange`
- 7 storage drivers: Local, S3, GCS, Azure, SFTP, Memory, ZIP
- Decorator pattern: Encryption, Validation, Caching, ReadOnly
- MountManager for virtual path namespacing
- FileValidator submodule with 60+ format validators
