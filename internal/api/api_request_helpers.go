package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/benpsk/go-starter/internal/apperror"
)

// defaultRequestBodyLimitBytes caps how much JSON a handler will read.
const defaultRequestBodyLimitBytes = 1 << 20 // 1 MiB

// decodeJSON reads a size-limited JSON body into dst. It returns an
// *apperror.AppError ready to hand to Error, or io.EOF for an empty body so
// callers can treat a missing body as optional.
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(w, r.Body, defaultRequestBodyLimitBytes)

	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			return apperror.New(http.StatusRequestEntityTooLarge, "request body too large", err)
		}
		if errors.Is(err, io.EOF) {
			return io.EOF
		}
		return apperror.BadRequest("invalid json")
	}
	return nil
}
