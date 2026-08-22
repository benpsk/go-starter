package apperror

import (
	"errors"
	"net/http"
	"testing"
)

func TestAppErrorWrapsCause(t *testing.T) {
	cause := errors.New("db down")
	err := New(http.StatusInternalServerError, "failed", cause)

	if err.Error() != "failed: db down" {
		t.Fatalf("unexpected error string: %q", err.Error())
	}
	if !errors.Is(err, cause) {
		t.Fatalf("expected the cause to unwrap")
	}
	if got := BadRequest("plain").Error(); got != "plain" {
		t.Fatalf("unexpected causeless error string: %q", got)
	}
}

func TestWithCode(t *testing.T) {
	err := BadRequest("bad")
	if WithCode(err, "bad_input") != err || err.Code != "bad_input" {
		t.Fatalf("expected the code to be applied in place: %+v", err)
	}
	if WithCode(nil, "ignored") != nil {
		t.Fatalf("expected nil to stay nil")
	}
}

func TestConstructorStatuses(t *testing.T) {
	cause := errors.New("boom")
	cases := []struct {
		name   string
		err    *AppError
		status int
	}{
		{"validation", Validation("invalid", map[string]string{"name": "required"}), http.StatusUnprocessableEntity},
		{"not found", NotFound("missing"), http.StatusNotFound},
		{"bad request", BadRequest("bad"), http.StatusBadRequest},
		{"unauthorized", Unauthorized("login"), http.StatusUnauthorized},
		{"forbidden", Forbidden("denied"), http.StatusForbidden},
		{"conflict", Conflict("taken"), http.StatusConflict},
		{"too many requests", TooManyRequests("slow down"), http.StatusTooManyRequests},
		{"unavailable", Unavailable("not configured"), http.StatusServiceUnavailable},
		{"internal", Internal("boom", cause), http.StatusInternalServerError},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.err.Status != tc.status {
				t.Fatalf("expected status %d, got %d", tc.status, tc.err.Status)
			}
			if tc.err.Message == "" {
				t.Fatalf("expected a message")
			}
		})
	}
}

func TestValidationCarriesDetails(t *testing.T) {
	err := Validation("invalid", map[string]string{"email": "required"})
	if err.Details["email"] != "required" {
		t.Fatalf("expected field details to survive: %+v", err.Details)
	}
}
