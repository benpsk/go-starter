package api_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/benpsk/go-starter/internal/api"
	"github.com/benpsk/go-starter/internal/apperror"
	"github.com/benpsk/go-starter/internal/appsvc"
	"github.com/benpsk/go-starter/internal/config"
	"github.com/go-chi/chi/v5"
)

func decodeBody(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal body %q: %v", rec.Body.String(), err)
	}
	return body
}

func requireEnvelopeFields(t *testing.T, body map[string]any) {
	t.Helper()
	if len(body) != 3 {
		t.Fatalf("expected exactly three envelope fields, got %#v", body)
	}
	for _, field := range []string{"data", "error", "errors"} {
		if _, ok := body[field]; !ok {
			t.Fatalf("missing %q in %#v", field, body)
		}
	}
}

func TestErrorUsesDocumentedEnvelope(t *testing.T) {
	rec := httptest.NewRecorder()
	api.Error(rec, apperror.NotFound("user not found"))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
	body := decodeBody(t, rec)
	requireEnvelopeFields(t, body)
	if body["data"] != nil || body["errors"] != nil {
		t.Fatalf("error response must have null data and errors: %s", rec.Body.String())
	}
	detail := body["error"].(map[string]any)
	if detail["code"] != "not_found" || detail["message"] != "user not found" {
		t.Fatalf("unexpected error: %#v", detail)
	}
}

func TestErrorIncludesValidationDetailsAsArrays(t *testing.T) {
	rec := httptest.NewRecorder()
	api.Error(rec, apperror.Validation("Please check the form fields", map[string]string{"code": "Code is required"}))

	body := decodeBody(t, rec)
	requireEnvelopeFields(t, body)
	if body["error"].(map[string]any)["code"] != "validation_error" {
		t.Fatalf("unexpected error: %s", rec.Body.String())
	}
	messages := body["errors"].(map[string]any)["code"].([]any)
	if len(messages) != 1 || messages[0] != "Code is required" {
		t.Fatalf("unexpected validation errors: %s", rec.Body.String())
	}
}

func TestErrorPreservesExplicitCode(t *testing.T) {
	rec := httptest.NewRecorder()
	api.Error(rec, apperror.WithCode(apperror.BadRequest("invalid"), "invalid_request"))
	if decodeBody(t, rec)["error"].(map[string]any)["code"] != "invalid_request" {
		t.Fatalf("unexpected body: %s", rec.Body.String())
	}
}

func TestErrorHidesInternalFailures(t *testing.T) {
	for _, err := range []error{
		errors.New("connection string postgres://user:secret@host/db failed"),
		apperror.Internal("failed to load user", errors.New("pq: password authentication failed")),
	} {
		rec := httptest.NewRecorder()
		api.Error(rec, err)
		if rec.Code != http.StatusInternalServerError || strings.Contains(rec.Body.String(), "secret") || strings.Contains(rec.Body.String(), "password") {
			t.Fatalf("internal detail reached the client: %s", rec.Body.String())
		}
	}
}

func TestSuccessUsesDocumentedEnvelope(t *testing.T) {
	tests := []struct {
		name    string
		payload any
		check   func(*testing.T, map[string]any)
	}{
		{"resource", map[string]any{"id": 1}, func(t *testing.T, data map[string]any) {
			if data["id"] != float64(1) {
				t.Fatalf("unexpected data: %#v", data)
			}
		}},
		{"message", "Operation completed successfully", func(t *testing.T, data map[string]any) {
			if data["message"] != "Operation completed successfully" {
				t.Fatalf("unexpected data: %#v", data)
			}
		}},
		{"pagination", api.PaginatedData{Items: []int{1, 2}, Pagination: api.Pagination{Total: 42, Page: 2, PerPage: 20, LastPage: 3}}, func(t *testing.T, data map[string]any) {
			if data["pagination"].(map[string]any)["last_page"] != float64(3) {
				t.Fatalf("unexpected data: %#v", data)
			}
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			api.Success(rec, http.StatusOK, tc.payload)
			body := decodeBody(t, rec)
			requireEnvelopeFields(t, body)
			if body["error"] != nil || body["errors"] != nil {
				t.Fatalf("unexpected body: %s", rec.Body.String())
			}
			tc.check(t, body["data"].(map[string]any))
		})
	}
}

func TestAPIRouterErrorsUseEnvelope(t *testing.T) {
	tests := []struct {
		name, method, path, contentType string
		status                          int
	}{
		{"method not allowed", http.MethodGet, "/api/auth/logout", "", http.StatusMethodNotAllowed},
		{"unsupported content type", http.MethodPost, "/api/auth/refresh", "application/x-www-form-urlencoded", http.StatusUnsupportedMediaType},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			var body *strings.Reader
			if tc.contentType == "" {
				body = strings.NewReader("")
			} else {
				body = strings.NewReader("a=b")
			}
			req := httptest.NewRequest(tc.method, tc.path, body)
			if tc.contentType != "" {
				req.Header.Set("Content-Type", tc.contentType)
			}
			testAPIRouter().ServeHTTP(rec, req)
			if rec.Code != tc.status {
				t.Fatalf("expected %d, got %d (%s)", tc.status, rec.Code, rec.Body.String())
			}
			requireEnvelopeFields(t, decodeBody(t, rec))
		})
	}
}

func testAPIRouter() http.Handler {
	r := chi.NewRouter()
	api.Mount(r, config.Config{
		AppURL: "http://127.0.0.1:8080", AppEnv: "test",
		Auth:      config.AuthConfig{API: config.APIAuthConfig{RefreshCookieName: "refresh"}},
		RateLimit: config.RateLimitConfig{Window: time.Minute, APILogin: 100, APIRefresh: 100},
	}, appsvc.Services{})
	return r
}
