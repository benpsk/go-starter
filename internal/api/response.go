package api

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"github.com/benpsk/go-starter/internal/apperror"
)

// Response wraps a list payload.
type Response[T any] struct {
	Data []T `json:"data"`
}

// ResponseMessage wraps a single-object payload.
type ResponseMessage[T any] struct {
	Data T `json:"data"`
}

// PageResponse wraps a paginated list payload.
type PageResponse[T any] struct {
	Data    []T `json:"data"`
	Page    int `json:"page"`
	PerPage int `json:"per_page"`
	Total   int `json:"total"`
}

// PaginationResponse is the untyped form of PageResponse, for handlers that
// assemble the payload dynamically.
type PaginationResponse struct {
	Data    any `json:"data"`
	Page    int `json:"page"`
	PerPage int `json:"per_page"`
	Total   int `json:"total"`
}

// ErrorResponse wraps an error payload.
type ErrorResponse[T any] struct {
	Errors T `json:"errors"`
}

// Error writes err as the API error envelope. An *apperror.AppError supplies
// the status, message, and optional field details; anything else is reported as
// an internal error and only logged, never echoed to the client.
func Error(w http.ResponseWriter, err error) {
	var appErr *apperror.AppError

	if errors.As(err, &appErr) {
		if appErr.Err != nil {
			log.Printf("api: internal error: %v", appErr.Err)
		}
		body := make(map[string]any)
		if appErr.Details != nil {
			body["errors"] = appErr.Details
		} else {
			body["errors"] = map[string]string{"message": appErr.Message}
		}
		if appErr.Code != "" {
			body["code"] = appErr.Code
		}
		toJSON(w, appErr.Status, body)
		return
	}

	log.Printf("api: unexpected error: %v", err)
	toJSON(w, http.StatusInternalServerError, map[string]any{
		"errors": map[string]string{"message": "an unexpected internal error occurred"},
	})
}

// Success writes payload as the API success envelope. A string becomes
// {"message": ...}; an already-shaped envelope passes through; anything else is
// wrapped as {"data": ...}.
func Success(w http.ResponseWriter, code int, payload any) {
	var envelope any

	switch value := payload.(type) {
	case nil:
		w.WriteHeader(code)
		return
	case string:
		envelope = map[string]any{"message": value}
	case PaginationResponse:
		envelope = value
	default:
		envelope = map[string]any{"data": payload}
	}
	toJSON(w, code, envelope)
}

func toJSON(w http.ResponseWriter, code int, payload any) {
	body, err := json.Marshal(payload)
	if err != nil {
		log.Printf("api: could not marshal json response: %v", err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_, _ = w.Write(body)
}
