package errors

import (
	"errors"
	"fmt"
)

// Define error types for better error handling
var (
	ErrNotFound         = errors.New("not found")
	ErrInvalidInput     = errors.New("invalid input")
	ErrUnauthorized     = errors.New("unauthorized")
	ErrForbidden        = errors.New("forbidden")
	ErrInternal         = errors.New("internal error")
	ErrNotImplemented   = errors.New("not implemented")
	ErrAlreadyExists    = errors.New("already exists")
	ErrTimeout          = errors.New("timeout")
	ErrTemporaryFailure = errors.New("temporary failure")
)

// AppError is a custom error type that includes error context
type AppError struct {
	Err     error
	Message string
	Code    string
	Op      string
}

// Error returns the error message
func (e *AppError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return e.Err.Error()
}

// Unwrap returns the wrapped error
func (e *AppError) Unwrap() error {
	return e.Err
}

// New creates a new error with context
func New(err error, op, code, message string) *AppError {
	return &AppError{
		Err:     err,
		Op:      op,
		Code:    code,
		Message: message,
	}
}

// Errorf formats an error message and wraps it in an AppError
func Errorf(format string, args ...interface{}) *AppError {
	return &AppError{
		Err:     fmt.Errorf(format, args...),
		Message: fmt.Sprintf(format, args...),
	}
}

// Is checks if the target error is of the given type
func Is(err, target error) bool {
	return errors.Is(err, target)
}

// As finds the first error in err's chain that matches target
func As(err error, target interface{}) bool {
	return errors.As(err, target)
}
