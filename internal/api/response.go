package api

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"github.com/benpsk/go-starter/internal/apperror"
)

// Envelope is the response shape shared by every JSON API endpoint.
type Envelope struct {
	Data   any                 `json:"data"`
	Error  *ErrorDetail        `json:"error"`
	Errors map[string][]string `json:"errors"`
}

// ErrorDetail describes an API error in a stable, client-readable form.
type ErrorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Pagination describes a page of results.
type Pagination struct {
	Total    int `json:"total"`
	Page     int `json:"page"`
	PerPage  int `json:"per_page"`
	LastPage int `json:"last_page"`
}

// PaginatedData is the data payload for a paginated success response.
type PaginatedData struct {
	Items      any        `json:"items"`
	Pagination Pagination `json:"pagination"`
}

// Error writes err using the API response envelope. Internal causes are logged
// but never exposed to the client.
func Error(w http.ResponseWriter, err error) {
	var appErr *apperror.AppError
	if !errors.As(err, &appErr) {
		log.Printf("api: unexpected error: %v", err)
		writeError(w, http.StatusInternalServerError, "server_error", "Something went wrong", nil)
		return
	}

	if appErr.Err != nil {
		log.Printf("api: internal error: %v", appErr.Err)
	}
	code := appErr.Code
	if code == "" {
		code = errorCode(appErr.Status)
	}
	details := make(map[string][]string, len(appErr.Details))
	for field, message := range appErr.Details {
		details[field] = []string{message}
	}
	if len(details) == 0 {
		details = nil
	}
	writeError(w, appErr.Status, code, appErr.Message, details)
}

// Success writes payload using the API response envelope. A string is treated
// as the required message payload for a success without resource data.
func Success(w http.ResponseWriter, status int, payload any) {
	if message, ok := payload.(string); ok {
		payload = map[string]string{"message": message}
	}
	toJSON(w, status, Envelope{Data: payload})
}

func writeError(w http.ResponseWriter, status int, code, message string, details map[string][]string) {
	toJSON(w, status, Envelope{
		Error:  &ErrorDetail{Code: code, Message: message},
		Errors: details,
	})
}

func errorCode(status int) string {
	switch status {
	case http.StatusBadRequest:
		return "bad_request"
	case http.StatusUnauthorized:
		return "unauthorized"
	case http.StatusForbidden:
		return "forbidden"
	case http.StatusNotFound:
		return "not_found"
	case http.StatusMethodNotAllowed:
		return "method_not_allowed"
	case http.StatusConflict:
		return "conflict"
	case http.StatusUnsupportedMediaType:
		return "unsupported_media_type"
	case http.StatusUnprocessableEntity:
		return "validation_error"
	case http.StatusRequestEntityTooLarge:
		return "request_too_large"
	case http.StatusTooManyRequests:
		return "too_many_requests"
	case http.StatusServiceUnavailable:
		return "service_unavailable"
	default:
		return "server_error"
	}
}

func toJSON(w http.ResponseWriter, status int, payload Envelope) {
	body, err := json.Marshal(payload)
	if err != nil {
		log.Printf("api: could not marshal json response: %v", err)
		body = []byte(`{"data":null,"error":{"code":"server_error","message":"Something went wrong"},"errors":null}`)
		status = http.StatusInternalServerError
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}
