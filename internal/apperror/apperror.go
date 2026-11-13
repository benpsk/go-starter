// Package apperror provides the error type shared by every transport in this
// application. Domain services describe failures semantically; transports
// choose how to render them.
package apperror

import "fmt"

// Kind identifies an application failure independently of its transport.
type Kind string

// Application failure kinds.
const (
	Invalid               Kind = "invalid"
	Unauthenticated       Kind = "unauthenticated"
	PermissionDenied      Kind = "permission_denied"
	Missing               Kind = "missing"
	MethodNotAllowed      Kind = "method_not_allowed"
	AlreadyExists         Kind = "already_exists"
	UnsupportedMediaType  Kind = "unsupported_media_type"
	InvalidFields         Kind = "invalid_fields"
	TooLarge              Kind = "too_large"
	RateLimited           Kind = "rate_limited"
	UnavailableDependency Kind = "unavailable_dependency"
	Unexpected            Kind = "unexpected"
)

// AppError carries the failure kind, the user-facing message, and the internal
// cause for a failed operation.
type AppError struct {
	Kind    Kind              // Semantic category of the failure
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

// New builds an AppError from a kind, message, and optional cause.
func New(kind Kind, message string, err error) *AppError {
	return &AppError{
		Kind:    kind,
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

// Validation constructs an application error.
func Validation(message string, details map[string]string) *AppError {
	return &AppError{
		Kind:    InvalidFields,
		Message: message,
		Details: details,
	}
}

// NotFound constructs an application error.
func NotFound(message string) *AppError {
	return New(Missing, message, nil)
}

// BadRequest constructs an application error.
func BadRequest(message string) *AppError {
	return New(Invalid, message, nil)
}

// Unauthorized constructs an application error.
func Unauthorized(message string) *AppError {
	return New(Unauthenticated, message, nil)
}

// Forbidden constructs an application error.
func Forbidden(message string) *AppError {
	return New(PermissionDenied, message, nil)
}

// Conflict constructs an application error.
func Conflict(message string) *AppError {
	return New(AlreadyExists, message, nil)
}

// TooManyRequests constructs an application error.
func TooManyRequests(message string) *AppError {
	return New(RateLimited, message, nil)
}

// Unavailable constructs an application error.
func Unavailable(message string) *AppError {
	return New(UnavailableDependency, message, nil)
}

// Internal constructs an application error.
func Internal(message string, err error) *AppError {
	return New(Unexpected, message, err)
}
