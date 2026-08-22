// Package apperror provides the error type shared by every transport in this
// application. Domain services return an *AppError so handlers can translate a
// failure into an HTTP status without inspecting concrete error values.
package apperror

import (
	"fmt"
	"net/http"
)

// AppError carries the HTTP status, the user-facing message, and the internal
// cause for a failed operation.
type AppError struct {
	Status  int               // HTTP status code (e.g. 400, 404, 500)
	Message string            // High-level, user-facing message
	Code    string            // Optional stable machine-readable error code
	Err     error             // Original internal error, for logging only
	Details map[string]string // Field-level validation errors
}

func (e *AppError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %v", e.Message, e.Err)
	}
	return e.Message
}

// Unwrap lets errors.Is and errors.As reach the wrapped cause.
func (e *AppError) Unwrap() error {
	return e.Err
}

// New builds an AppError from a status, message, and optional cause.
func New(status int, message string, err error) *AppError {
	return &AppError{
		Status:  status,
		Message: message,
		Err:     err,
	}
}

// WithCode attaches a machine-readable code to an existing error in place.
func WithCode(err *AppError, code string) *AppError {
	if err == nil {
		return nil
	}
	err.Code = code
	return err
}

// Validation reports field-level input errors as 422.
func Validation(message string, details map[string]string) *AppError {
	return &AppError{
		Status:  http.StatusUnprocessableEntity,
		Message: message,
		Details: details,
	}
}

// NotFound reports a missing resource as 404.
func NotFound(message string) *AppError {
	return New(http.StatusNotFound, message, nil)
}

// BadRequest reports a malformed request as 400.
func BadRequest(message string) *AppError {
	return New(http.StatusBadRequest, message, nil)
}

// Unauthorized reports a missing or invalid credential as 401.
func Unauthorized(message string) *AppError {
	return New(http.StatusUnauthorized, message, nil)
}

// Forbidden reports an authenticated but unpermitted request as 403.
func Forbidden(message string) *AppError {
	return New(http.StatusForbidden, message, nil)
}

// Conflict reports a uniqueness or state conflict as 409.
func Conflict(message string) *AppError {
	return New(http.StatusConflict, message, nil)
}

// TooManyRequests reports a throttled request as 429.
func TooManyRequests(message string) *AppError {
	return New(http.StatusTooManyRequests, message, nil)
}

// Unavailable reports an unconfigured or unreachable dependency as 503.
func Unavailable(message string) *AppError {
	return New(http.StatusServiceUnavailable, message, nil)
}

// Internal reports an unexpected failure as 500 while retaining the cause.
func Internal(message string, err error) *AppError {
	return New(http.StatusInternalServerError, message, err)
}
