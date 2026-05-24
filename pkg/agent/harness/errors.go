package harness

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"
)

type FileErrorCode string

const (
	FileErrorAborted          FileErrorCode = "aborted"
	FileErrorNotFound         FileErrorCode = "not_found"
	FileErrorPermissionDenied FileErrorCode = "permission_denied"
	FileErrorNotDirectory     FileErrorCode = "not_directory"
	FileErrorIsDirectory      FileErrorCode = "is_directory"
	FileErrorInvalid          FileErrorCode = "invalid"
	FileErrorNotSupported     FileErrorCode = "not_supported"
	FileErrorUnknown          FileErrorCode = "unknown"
)

type FileError struct {
	Code  FileErrorCode
	Path  string
	Cause error
}

func NewFileError(code FileErrorCode, message string, path string, cause error) *FileError {
	if cause == nil {
		cause = errors.New(message)
	}
	return &FileError{Code: code, Path: path, Cause: cause}
}

func (e *FileError) Error() string {
	if e.Path == "" {
		return e.Cause.Error()
	}
	return fmt.Sprintf("%s: %s", e.Path, e.Cause.Error())
}

func (e *FileError) Unwrap() error {
	return e.Cause
}

type ExecutionErrorCode string

const (
	ExecutionErrorAborted          ExecutionErrorCode = "aborted"
	ExecutionErrorTimeout          ExecutionErrorCode = "timeout"
	ExecutionErrorShellUnavailable ExecutionErrorCode = "shell_unavailable"
	ExecutionErrorSpawnError       ExecutionErrorCode = "spawn_error"
	ExecutionErrorCallbackError    ExecutionErrorCode = "callback_error"
	ExecutionErrorUnknown          ExecutionErrorCode = "unknown"
)

type ExecutionError struct {
	Code  ExecutionErrorCode
	Cause error
}

func NewExecutionError(code ExecutionErrorCode, message string, cause error) *ExecutionError {
	if cause == nil {
		cause = errors.New(message)
	}
	return &ExecutionError{Code: code, Cause: cause}
}

func (e *ExecutionError) Error() string {
	return e.Cause.Error()
}

func (e *ExecutionError) Unwrap() error {
	return e.Cause
}

func toFileError(err error, path string) *FileError {
	if err == nil {
		return nil
	}
	var fileErr *FileError
	if errors.As(err, &fileErr) {
		return fileErr
	}
	code := FileErrorUnknown
	switch {
	case errors.Is(err, context.Canceled):
		code = FileErrorAborted
	case errors.Is(err, os.ErrNotExist):
		code = FileErrorNotFound
	case errors.Is(err, os.ErrPermission):
		code = FileErrorPermissionDenied
	case errors.Is(err, syscall.ENOTDIR):
		code = FileErrorNotDirectory
	case errors.Is(err, syscall.EISDIR):
		code = FileErrorIsDirectory
	case errors.Is(err, os.ErrInvalid), errors.Is(err, syscall.EINVAL):
		code = FileErrorInvalid
	}
	return NewFileError(code, err.Error(), path, err)
}

func toExecutionError(err error) *ExecutionError {
	if err == nil {
		return nil
	}
	var execErr *ExecutionError
	if errors.As(err, &execErr) {
		return execErr
	}
	code := ExecutionErrorUnknown
	switch {
	case errors.Is(err, context.Canceled):
		code = ExecutionErrorAborted
	case errors.Is(err, context.DeadlineExceeded):
		code = ExecutionErrorTimeout
	}
	var pathErr *exec.Error
	if errors.As(err, &pathErr) {
		code = ExecutionErrorShellUnavailable
	}
	var osPathErr *os.PathError
	if errors.As(err, &osPathErr) && code == ExecutionErrorUnknown {
		code = ExecutionErrorSpawnError
	}
	return NewExecutionError(code, err.Error(), err)
}
