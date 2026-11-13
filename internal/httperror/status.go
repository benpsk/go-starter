// Package httperror maps application failures to HTTP responses.
package httperror

import (
	"net/http"

	"github.com/benpsk/go-starter/internal/apperror"
)

// Status returns the HTTP status for a failure kind. Unknown kinds are internal errors.
func Status(kind apperror.Kind) int {
	switch kind {
	case apperror.Invalid:
		return http.StatusBadRequest
	case apperror.Unauthenticated:
		return http.StatusUnauthorized
	case apperror.PermissionDenied:
		return http.StatusForbidden
	case apperror.Missing:
		return http.StatusNotFound
	case apperror.MethodNotAllowed:
		return http.StatusMethodNotAllowed
	case apperror.AlreadyExists:
		return http.StatusConflict
	case apperror.UnsupportedMediaType:
		return http.StatusUnsupportedMediaType
	case apperror.InvalidFields:
		return http.StatusUnprocessableEntity
	case apperror.TooLarge:
		return http.StatusRequestEntityTooLarge
	case apperror.RateLimited:
		return http.StatusTooManyRequests
	case apperror.UnavailableDependency:
		return http.StatusServiceUnavailable
	case apperror.Unexpected:
		return http.StatusInternalServerError
	default:
		return http.StatusInternalServerError
	}
}
