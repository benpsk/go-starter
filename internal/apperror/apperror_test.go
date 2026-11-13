package apperror

import (
	"errors"
	"testing"
)

func TestAppErrorWrapsCause(t *testing.T) {
	cause := errors.New("db down")
	err := New(Unexpected, "failed", cause)

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

func TestConstructorKinds(t *testing.T) {
	cause := errors.New("boom")
	cases := []struct {
		name string
		err  *AppError
		kind Kind
	}{
		{"validation", Validation("invalid", map[string]string{"name": "required"}), InvalidFields},
		{"not found", NotFound("missing"), Missing},
		{"bad request", BadRequest("bad"), Invalid},
		{"unauthorized", Unauthorized("login"), Unauthenticated},
		{"forbidden", Forbidden("denied"), PermissionDenied},
		{"conflict", Conflict("taken"), AlreadyExists},
		{"too many requests", TooManyRequests("slow down"), RateLimited},
		{"unavailable", Unavailable("not configured"), UnavailableDependency},
		{"internal", Internal("boom", cause), Unexpected},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.err.Kind != tc.kind {
				t.Fatalf("expected kind %s, got %s", tc.kind, tc.err.Kind)
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
