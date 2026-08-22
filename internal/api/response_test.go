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

func TestErrorUsesAppErrorStatusAndMessage(t *testing.T) {
	rec := httptest.NewRecorder()
	api.Error(rec, apperror.NotFound("user not found"))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Fatalf("unexpected content type: %q", got)
	}
	errorsField, ok := decodeBody(t, rec)["errors"].(map[string]any)
	if !ok {
		t.Fatalf("expected an errors object, got %s", rec.Body.String())
	}
	if errorsField["message"] != "user not found" {
		t.Fatalf("unexpected message: %v", errorsField["message"])
	}
}

func TestErrorIncludesValidationDetailsAndCode(t *testing.T) {
	rec := httptest.NewRecorder()
	api.Error(rec, apperror.WithCode(
		apperror.Validation("invalid", map[string]string{"code": "required"}),
		"invalid_request",
	))

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d", rec.Code)
	}
	body := decodeBody(t, rec)
	if body["code"] != "invalid_request" {
		t.Fatalf("expected the code to be surfaced, got %v", body["code"])
	}
	details, ok := body["errors"].(map[string]any)
	if !ok || details["code"] != "required" {
		t.Fatalf("expected field details, got %s", rec.Body.String())
	}
}

func TestErrorHidesUnexpectedFailures(t *testing.T) {
	rec := httptest.NewRecorder()
	api.Error(rec, errors.New("connection string postgres://user:secret@host/db failed"))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
	}
	if body := rec.Body.String(); body == "" || strings.Contains(body, "secret") {
		t.Fatalf("internal detail must not reach the client: %s", body)
	}
}

func TestErrorHidesWrappedInternalCause(t *testing.T) {
	rec := httptest.NewRecorder()
	api.Error(rec, apperror.Internal("failed to load user", errors.New("pq: password authentication failed")))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "password") {
		t.Fatalf("the wrapped cause must stay in the logs: %s", rec.Body.String())
	}
}

func TestSuccessEnvelopes(t *testing.T) {
	t.Run("object is wrapped in data", func(t *testing.T) {
		rec := httptest.NewRecorder()
		api.Success(rec, http.StatusOK, map[string]any{"id": 1})

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
		data, ok := decodeBody(t, rec)["data"].(map[string]any)
		if !ok || data["id"] != float64(1) {
			t.Fatalf("unexpected body: %s", rec.Body.String())
		}
	})

	t.Run("string becomes a message", func(t *testing.T) {
		rec := httptest.NewRecorder()
		api.Success(rec, http.StatusOK, "deleted")

		if decodeBody(t, rec)["message"] != "deleted" {
			t.Fatalf("unexpected body: %s", rec.Body.String())
		}
	})

	t.Run("pagination envelope passes through", func(t *testing.T) {
		rec := httptest.NewRecorder()
		api.Success(rec, http.StatusOK, api.PaginationResponse{Data: []int{1, 2}, Page: 2, PerPage: 20, Total: 42})

		body := decodeBody(t, rec)
		if body["page"] != float64(2) || body["per_page"] != float64(20) || body["total"] != float64(42) {
			t.Fatalf("expected the envelope to pass through unwrapped: %s", rec.Body.String())
		}
	})

	t.Run("nil writes only a status", func(t *testing.T) {
		rec := httptest.NewRecorder()
		api.Success(rec, http.StatusNoContent, nil)

		if rec.Code != http.StatusNoContent {
			t.Fatalf("expected 204, got %d", rec.Code)
		}
		if rec.Body.Len() != 0 {
			t.Fatalf("expected an empty body, got %s", rec.Body.String())
		}
	})
}

func TestAPIMethodNotAllowed(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/auth/logout", nil)
	testAPIRouter().ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d (%s)", rec.Code, rec.Body.String())
	}
	if !strings.HasPrefix(rec.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("an api 405 must stay json, got %q", rec.Header().Get("Content-Type"))
	}
}

func TestAPIRejectsNonJSONContentType(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/auth/refresh", strings.NewReader("a=b"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	testAPIRouter().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("expected 415, got %d (%s)", rec.Code, rec.Body.String())
	}
}

// testAPIRouter mounts the API with services that are never reached, so the
// routing and middleware layers can be checked on their own.
func testAPIRouter() http.Handler {
	r := chi.NewRouter()
	api.Mount(r, config.Config{
		AppURL:    "http://127.0.0.1:8080",
		AppEnv:    "test",
		Auth:      config.AuthConfig{API: config.APIAuthConfig{RefreshCookieName: "refresh"}},
		RateLimit: config.RateLimitConfig{Window: time.Minute, APILogin: 100, APIRefresh: 100},
	}, appsvc.Services{})
	return r
}
