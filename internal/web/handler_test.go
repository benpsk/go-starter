package web_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/benpsk/go-starter/internal/apperror"
	"github.com/benpsk/go-starter/internal/web"
)

func TestIsHtmx(t *testing.T) {
	cases := []struct {
		name   string
		header string
		want   bool
	}{
		{"htmx request", "true", true},
		{"other value", "false", false},
		{"absent", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			if tc.header != "" {
				r.Header.Set("HX-Request", tc.header)
			}
			if got := web.IsHtmx(r); got != tc.want {
				t.Fatalf("IsHtmx() = %v, want %v", got, tc.want)
			}
		})
	}
	if web.IsHtmx(nil) {
		t.Fatalf("expected false for a nil request")
	}
}

func TestErrorEnvelope(t *testing.T) {
	rec := httptest.NewRecorder()
	web.Error(rec, apperror.Validation("invalid", map[string]string{"name": "required"}))

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d", rec.Code)
	}
	var body struct {
		Errors map[string]string `json:"errors"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal %q: %v", rec.Body.String(), err)
	}
	if body.Errors["name"] != "required" {
		t.Fatalf("expected the field details, got %s", rec.Body.String())
	}
}

func TestErrorHidesUnexpectedFailures(t *testing.T) {
	rec := httptest.NewRecorder()
	web.Error(rec, errors.New("dsn postgres://user:hunter2@db/app"))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "hunter2") {
		t.Fatalf("internal detail must not reach the client: %s", rec.Body.String())
	}
}

func TestSuccessEnvelope(t *testing.T) {
	t.Run("object", func(t *testing.T) {
		rec := httptest.NewRecorder()
		web.Success(rec, http.StatusOK, map[string]any{"id": 2})

		var body struct {
			Data map[string]any `json:"data"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("unmarshal %q: %v", rec.Body.String(), err)
		}
		if body.Data["id"] != float64(2) {
			t.Fatalf("unexpected body: %s", rec.Body.String())
		}
	})

	t.Run("string", func(t *testing.T) {
		rec := httptest.NewRecorder()
		web.Success(rec, http.StatusOK, "saved")

		var body struct {
			Message string `json:"message"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("unmarshal %q: %v", rec.Body.String(), err)
		}
		if body.Message != "saved" {
			t.Fatalf("unexpected body: %s", rec.Body.String())
		}
	})
}
