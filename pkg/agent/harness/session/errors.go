package session

import "errors"

type SessionErrorCode string

const (
	SessionErrorNotFound          SessionErrorCode = "not_found"
	SessionErrorInvalidSession    SessionErrorCode = "invalid_session"
	SessionErrorInvalidEntry      SessionErrorCode = "invalid_entry"
	SessionErrorInvalidForkTarget SessionErrorCode = "invalid_fork_target"
	SessionErrorStorage           SessionErrorCode = "storage"
	SessionErrorUnknown           SessionErrorCode = "unknown"
)

type SessionError struct {
	Code  SessionErrorCode
	Cause error
}

func NewSessionError(code SessionErrorCode, message string, cause error) *SessionError {
	if cause == nil {
		cause = errors.New(message)
	}
	return &SessionError{Code: code, Cause: cause}
}

func (e *SessionError) Error() string { return e.Cause.Error() }
func (e *SessionError) Unwrap() error { return e.Cause }
