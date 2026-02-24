package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
)

const (
	defaultRequestBodyLimitBytes = 1 << 20 // 1 MiB
)

func limitRequestBody(w http.ResponseWriter, r *http.Request, maxBytes int64) {
	if maxBytes <= 0 {
		maxBytes = defaultRequestBodyLimitBytes
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
}

func parseFormWithLimit(w http.ResponseWriter, r *http.Request, maxBytes int64) error {
	limitRequestBody(w, r, maxBytes)
	if err := r.ParseForm(); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			return fmt.Errorf("request body too large: %w", err)
		}
		return err
	}
	return nil
}

func decodeJSONWithLimit(w http.ResponseWriter, r *http.Request, dst any, maxBytes int64) error {
	limitRequestBody(w, r, maxBytes)
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			return fmt.Errorf("request body too large: %w", err)
		}
		if errors.Is(err, io.EOF) {
			return io.EOF
		}
		return err
	}
	return nil
}

func isRequestBodyTooLarge(err error) bool {
	var maxErr *http.MaxBytesError
	return errors.As(err, &maxErr)
}
