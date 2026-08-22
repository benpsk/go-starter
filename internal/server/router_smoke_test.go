package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	usersession "github.com/benpsk/go-starter/internal/auth/user"
	"github.com/benpsk/go-starter/internal/config"
	healthsvc "github.com/benpsk/go-starter/internal/health"
	loginsvc "github.com/benpsk/go-starter/internal/login"
	"github.com/benpsk/go-starter/internal/storage"
	usersvc "github.com/benpsk/go-starter/internal/users"
)

// The route tree is exercised end to end with in-memory repositories, so this
// covers global middleware, mounting, and the transport split without needing a
// database. The repositories' own SQL is covered in internal/postgres.

type healthRepoStub struct{ err error }

func (r healthRepoStub) Ping(context.Context) error { return r.err }

type userRepoStub struct{}

func (userRepoStub) FindByID(context.Context, int64) (usersvc.User, error) {
	return usersvc.User{}, usersvc.ErrNotFound
}

func (userRepoStub) FindByEmail(context.Context, string) (usersvc.User, error) {
	return usersvc.User{}, usersvc.ErrNotFound
}

func (userRepoStub) FindByIdentity(context.Context, string, string) (usersvc.User, error) {
	return usersvc.User{}, usersvc.ErrNotFound
}

func (userRepoStub) ListIdentitiesByUserID(context.Context, int64) ([]usersvc.Identity, error) {
	return nil, nil
}

func (userRepoStub) CreateWithIdentity(context.Context, usersvc.SocialProfile) (usersvc.User, error) {
	return usersvc.User{}, errors.New("not implemented")
}

func (userRepoStub) UpdateFromProfile(context.Context, int64, usersvc.SocialProfile) error {
	return nil
}

type loginRepoStub struct{}

func (loginRepoStub) CreateRefreshToken(context.Context, loginsvc.RefreshToken) error { return nil }

func (loginRepoStub) RotateRefreshToken(context.Context, string, loginsvc.RefreshToken, time.Time) (loginsvc.RotationResult, error) {
	return loginsvc.RotationResult{}, nil
}

func (loginRepoStub) RevokeRefreshTokenByHash(context.Context, string, time.Time) error { return nil }

func (loginRepoStub) RevokeRefreshTokenFamily(context.Context, string, time.Time) error { return nil }

func (loginRepoStub) DeleteRefreshTokensBefore(context.Context, time.Time) (int64, error) {
	return 0, nil
}

func testRouter(t *testing.T, healthErr error) http.Handler {
	t.Helper()

	cfg := config.Config{
		AppName: "Go Starter",
		AppEnv:  "test",
		AppURL:  "http://127.0.0.1:8080",
		Auth: config.AuthConfig{
			SessionCookieName: "go_starter_session",
			SessionTTL:        time.Hour,
			API: config.APIAuthConfig{
				AccessTokenSecret: "test-secret",
				AccessTokenTTL:    10 * time.Minute,
				RefreshTokenTTL:   time.Hour,
				RefreshCookieName: "go_starter_api_refresh",
			},
		},
		RateLimit: config.RateLimitConfig{Window: time.Minute, APILogin: 100, APIRefresh: 100, WebOAuth: 100},
		Storage:   config.StorageConfig{Driver: config.StorageDriverLocal, LocalDir: t.TempDir(), LocalPublicPath: "/media"},
	}

	userService := usersvc.NewService(userRepoStub{})
	services := Services{
		Health:  healthsvc.NewService(healthRepoStub{err: healthErr}),
		Users:   userService,
		Login:   loginsvc.NewService(loginRepoStub{}, userService, nil, loginsvc.Config{AppURL: cfg.AppURL, AccessTokenSecret: "test-secret"}),
		Storage: storage.NewDisabledService("not configured in tests"),
	}

	return NewRouter(cfg, usersession.NewManager(nil, "go_starter_session", time.Hour, false), services)
}

func TestHealthEndpoints(t *testing.T) {
	cases := []struct {
		name       string
		path       string
		healthErr  error
		wantStatus int
		wantState  string
	}{
		{"liveness ignores dependencies", "/healthz", errors.New("db down"), http.StatusOK, healthsvc.StatusOK},
		{"readiness passes", "/readyz", nil, http.StatusOK, healthsvc.StatusOK},
		{"readiness reports a down database", "/readyz", errors.New("db down"), http.StatusServiceUnavailable, healthsvc.StatusDegraded},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			testRouter(t, tc.healthErr).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tc.path, nil))

			if rec.Code != tc.wantStatus {
				t.Fatalf("expected %d, got %d (%s)", tc.wantStatus, rec.Code, rec.Body.String())
			}
			var status healthsvc.Status
			if err := json.Unmarshal(rec.Body.Bytes(), &status); err != nil {
				t.Fatalf("unmarshal %q: %v", rec.Body.String(), err)
			}
			if status.Status != tc.wantState {
				t.Fatalf("expected state %q, got %q", tc.wantState, status.Status)
			}
		})
	}
}

func TestSecurityHeadersAreGlobal(t *testing.T) {
	router := testRouter(t, nil)

	for _, path := range []string{"/", "/healthz", "/api/auth/me"} {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))

		if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
			t.Fatalf("%s: expected nosniff, got %q", path, got)
		}
		if got := rec.Header().Get("Referrer-Policy"); got != "strict-origin-when-cross-origin" {
			t.Fatalf("%s: missing referrer policy", path)
		}
	}
}

func TestBrowserPagesRender(t *testing.T) {
	router := testRouter(t, nil)

	cases := []struct {
		path       string
		wantStatus int
		wantBody   string
	}{
		{"/", http.StatusOK, "Go Starter"},
		{"/about", http.StatusOK, "What this starter gives you"},
		{"/auth/login", http.StatusOK, "Sign in"},
		{"/nope", http.StatusNotFound, "Page not found"},
	}

	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tc.path, nil))

			if rec.Code != tc.wantStatus {
				t.Fatalf("expected %d, got %d", tc.wantStatus, rec.Code)
			}
			if !strings.Contains(rec.Body.String(), tc.wantBody) {
				t.Fatalf("expected the page to contain %q", tc.wantBody)
			}
			if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/html") {
				t.Fatalf("expected html, got %q", got)
			}
		})
	}
}

func TestMethodNotAllowedRendersThePage(t *testing.T) {
	rec := httptest.NewRecorder()
	testRouter(t, nil).ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/about", nil))

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Method not allowed") {
		t.Fatalf("expected the 405 page, got %s", rec.Body.String())
	}
}

func TestBrowserRoutesIssueAndEnforceCSRF(t *testing.T) {
	router := testRouter(t, nil)

	get := httptest.NewRecorder()
	router.ServeHTTP(get, httptest.NewRequest(http.MethodGet, "/auth/login", nil))

	var csrfCookie *http.Cookie
	for _, cookie := range get.Result().Cookies() {
		if cookie.Name == "csrf_token" {
			csrfCookie = cookie
		}
	}
	if csrfCookie == nil || csrfCookie.Value == "" {
		t.Fatalf("expected a csrf cookie to be issued on a browser GET")
	}

	t.Run("post without a token is rejected", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/auth/login/google", nil)
		req.AddCookie(csrfCookie)
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403, got %d", rec.Code)
		}
	})

	t.Run("post with a matching token passes csrf", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/auth/login/google", nil)
		req.AddCookie(csrfCookie)
		req.Header.Set("X-CSRF-Token", csrfCookie.Value)
		router.ServeHTTP(rec, req)

		// No provider is configured in this fixture, so the flow redirects
		// back to the login page rather than being blocked by CSRF.
		if rec.Code == http.StatusForbidden {
			t.Fatalf("a matching token must pass csrf")
		}
		if rec.Code != http.StatusSeeOther {
			t.Fatalf("expected a redirect, got %d", rec.Code)
		}
		if got := rec.Header().Get("Location"); !strings.Contains(got, "provider_not_configured") {
			t.Fatalf("unexpected redirect target: %q", got)
		}
	})
}

func TestProtectedBrowserRouteRedirectsAnonymousVisitors(t *testing.T) {
	rec := httptest.NewRecorder()
	testRouter(t, nil).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/account", nil))

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", rec.Code)
	}
	if got := rec.Header().Get("Location"); got != "/auth/login" {
		t.Fatalf("unexpected redirect target: %q", got)
	}
}

func TestAPIRoutesAreNotCSRFProtected(t *testing.T) {
	// The API authenticates with a bearer token rather than an ambient cookie,
	// so a POST without a CSRF token must reach the handler and fail on auth.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/auth/refresh", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	testRouter(t, nil).ServeHTTP(rec, req)

	if rec.Code == http.StatusForbidden {
		t.Fatalf("the api must not be behind browser csrf protection")
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for a missing refresh token, got %d (%s)", rec.Code, rec.Body.String())
	}
}

func TestAPIRequiresBearerToken(t *testing.T) {
	rec := httptest.NewRecorder()
	testRouter(t, nil).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/auth/me", nil))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d (%s)", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "application/json") {
		t.Fatalf("the api must answer with json, got %q", got)
	}
}

func TestAPIUnknownRouteAnswersWithJSON(t *testing.T) {
	rec := httptest.NewRecorder()
	testRouter(t, nil).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/nope", nil))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "application/json") {
		t.Fatalf("an api miss must stay json, got %q (%s)", got, rec.Body.String())
	}
}

func TestStaticAssetsAreServed(t *testing.T) {
	rec := httptest.NewRecorder()
	testRouter(t, nil).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/static/vendor/placeholder.txt", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected the embedded asset to be served, got %d", rec.Code)
	}
}
