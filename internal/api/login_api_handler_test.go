package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/benpsk/go-starter/internal/api"
	"github.com/benpsk/go-starter/internal/appsvc"
	"github.com/benpsk/go-starter/internal/config"
	loginsvc "github.com/benpsk/go-starter/internal/login"
	"github.com/benpsk/go-starter/internal/postgres"
	"github.com/benpsk/go-starter/internal/storage"
	"github.com/benpsk/go-starter/internal/testutil"
	usersvc "github.com/benpsk/go-starter/internal/users"
	"github.com/go-chi/chi/v5"
)

// These are feature tests: a real router, real services, and a real database.
// Only the OAuth provider is substituted, because the test cannot reach Google.

const refreshCookieName = "go_starter_api_refresh"

// providerStub returns a fixed verified profile in place of a real provider.
type providerStub struct {
	profile usersvc.SocialProfile
	err     error
}

func (p providerStub) ExchangeAndVerify(_ context.Context, provider, _, _, _ string, _ loginsvc.OAuthProviderConfig) (usersvc.SocialProfile, error) {
	if p.err != nil {
		return usersvc.SocialProfile{}, p.err
	}
	profile := p.profile
	if profile.Provider == "" {
		profile.Provider = provider
	}
	return profile, nil
}

func apiTestConfig() config.Config {
	return config.Config{
		AppName: "Go Starter",
		AppEnv:  "test",
		AppURL:  "http://127.0.0.1:8080",
		Auth: config.AuthConfig{
			API: config.APIAuthConfig{
				AccessTokenSecret: "test-secret",
				AccessTokenTTL:    10 * time.Minute,
				RefreshTokenTTL:   time.Hour,
				RefreshCookieName: refreshCookieName,
			},
			Social: config.SocialAuthConfig{
				Google: config.OAuthClientConfig{ClientID: "google-id", ClientSecret: "google-secret"},
			},
		},
		RateLimit: config.RateLimitConfig{Window: time.Minute, APILogin: 100, APIRefresh: 100, WebOAuth: 100},
	}
}

// newAPI mounts the JSON API over the test database. Every request must carry
// the returned context so the handler chain runs inside the rolled-back
// transaction.
func newAPI(t *testing.T, verifier loginsvc.SocialVerifier) (http.Handler, context.Context, func()) {
	t.Helper()

	pool := testutil.SetupDB(t)
	ctx, done := testutil.WithTx(t, pool)

	cfg := apiTestConfig()
	userService := usersvc.NewService(postgres.NewUserRepository(pool))
	loginService := loginsvc.NewService(
		postgres.NewLoginRepository(pool),
		userService,
		verifier,
		loginsvc.Config{
			AppURL:            cfg.AppURL,
			AccessTokenSecret: cfg.Auth.API.AccessTokenSecret,
			AccessTokenTTL:    cfg.Auth.API.AccessTokenTTL,
			RefreshTokenTTL:   cfg.Auth.API.RefreshTokenTTL,
			Google: loginsvc.OAuthProviderConfig{
				ClientID:     cfg.Auth.Social.Google.ClientID,
				ClientSecret: cfg.Auth.Social.Google.ClientSecret,
			},
		},
	)

	r := chi.NewRouter()
	api.Mount(r, cfg, appsvc.Services{
		Users:   userService,
		Login:   loginService,
		Storage: storage.NewDisabledService("not configured in tests"),
	})
	return r, ctx, done
}

func do(t *testing.T, handler http.Handler, ctx context.Context, method, path, body string, decorate func(*http.Request)) *httptest.ResponseRecorder {
	t.Helper()

	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader).WithContext(ctx)
	req.Header.Set("Content-Type", "application/json")
	if decorate != nil {
		decorate(req)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func decodeData(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal %q: %v", rec.Body.String(), err)
	}
	return body.Data
}

func loginBody() string {
	return `{"code":"auth-code","code_verifier":"verifier","redirect_uri":"app://callback"}`
}

func TestAPILoginIssuesTokensAndCreatesTheUser(t *testing.T) {
	handler, ctx, done := newAPI(t, providerStub{profile: usersvc.SocialProfile{
		ProviderUserID: "g-api-1",
		Email:          "api@example.com",
		EmailVerified:  true,
		Name:           "API User",
	}})
	defer done()

	rec := do(t, handler, ctx, http.MethodPost, "/api/auth/login/google", loginBody(), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}

	data := decodeData(t, rec)
	if data["access_token"] == "" || data["refresh_token"] == "" {
		t.Fatalf("expected a token pair: %s", rec.Body.String())
	}
	user, ok := data["user"].(map[string]any)
	if !ok || user["email"] != "api@example.com" || user["display_name"] != "API User" {
		t.Fatalf("unexpected user payload: %s", rec.Body.String())
	}

	var refreshCookie *http.Cookie
	for _, cookie := range rec.Result().Cookies() {
		if cookie.Name == refreshCookieName {
			refreshCookie = cookie
		}
	}
	if refreshCookie == nil {
		t.Fatalf("expected the refresh token to be mirrored in a cookie")
	}
	if !refreshCookie.HttpOnly || refreshCookie.Path != "/api/auth" {
		t.Fatalf("the refresh cookie must be HttpOnly and scoped: %+v", refreshCookie)
	}

	t.Run("the access token authenticates /me", func(t *testing.T) {
		me := do(t, handler, ctx, http.MethodGet, "/api/auth/me", "", func(r *http.Request) {
			r.Header.Set("Authorization", "Bearer "+data["access_token"].(string))
		})
		if me.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d (%s)", me.Code, me.Body.String())
		}
		if decodeData(t, me)["email"] != "api@example.com" {
			t.Fatalf("unexpected /me payload: %s", me.Body.String())
		}
	})

	t.Run("signing in again reuses the same account", func(t *testing.T) {
		again := do(t, handler, ctx, http.MethodPost, "/api/auth/login/google", loginBody(), nil)
		if again.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d (%s)", again.Code, again.Body.String())
		}
		firstID := user["id"]
		secondID := decodeData(t, again)["user"].(map[string]any)["id"]
		if firstID != secondID {
			t.Fatalf("expected the same account, got %v then %v", firstID, secondID)
		}
	})
}

func TestAPILoginRejectsBadRequests(t *testing.T) {
	handler, ctx, done := newAPI(t, providerStub{profile: usersvc.SocialProfile{ProviderUserID: "g-api-2"}})
	defer done()

	cases := []struct {
		name       string
		path       string
		body       string
		wantStatus int
	}{
		{"missing body", "/api/auth/login/google", "", http.StatusBadRequest},
		{"invalid json", "/api/auth/login/google", "{", http.StatusBadRequest},
		{"missing fields", "/api/auth/login/google", `{"code":"x"}`, http.StatusUnprocessableEntity},
		{"unknown provider", "/api/auth/login/apple", loginBody(), http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := do(t, handler, ctx, http.MethodPost, tc.path, tc.body, nil)
			if rec.Code != tc.wantStatus {
				t.Fatalf("expected %d, got %d (%s)", tc.wantStatus, rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), `"errors"`) {
				t.Fatalf("expected the error envelope, got %s", rec.Body.String())
			}
		})
	}
}

func TestAPIRefreshRotatesAndInvalidatesTheOldToken(t *testing.T) {
	handler, ctx, done := newAPI(t, providerStub{profile: usersvc.SocialProfile{
		ProviderUserID: "g-api-3",
		Email:          "refresh@example.com",
	}})
	defer done()

	login := do(t, handler, ctx, http.MethodPost, "/api/auth/login/google", loginBody(), nil)
	if login.Code != http.StatusOK {
		t.Fatalf("login failed: %d (%s)", login.Code, login.Body.String())
	}
	first := decodeData(t, login)["refresh_token"].(string)

	rotated := do(t, handler, ctx, http.MethodPost, "/api/auth/refresh", `{"refresh_token":"`+first+`"}`, nil)
	if rotated.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rotated.Code, rotated.Body.String())
	}
	second := decodeData(t, rotated)["refresh_token"].(string)
	if second == first {
		t.Fatalf("refresh must return a new token")
	}

	t.Run("the rotated token cannot be replayed", func(t *testing.T) {
		replay := do(t, handler, ctx, http.MethodPost, "/api/auth/refresh", `{"refresh_token":"`+first+`"}`, nil)
		if replay.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d (%s)", replay.Code, replay.Body.String())
		}
	})

	t.Run("reuse revokes the successor too", func(t *testing.T) {
		after := do(t, handler, ctx, http.MethodPost, "/api/auth/refresh", `{"refresh_token":"`+second+`"}`, nil)
		if after.Code != http.StatusUnauthorized {
			t.Fatalf("the whole family must be revoked, got %d (%s)", after.Code, after.Body.String())
		}
	})
}

func TestAPIRefreshAcceptsTheCookie(t *testing.T) {
	handler, ctx, done := newAPI(t, providerStub{profile: usersvc.SocialProfile{
		ProviderUserID: "g-api-4",
		Email:          "cookie@example.com",
	}})
	defer done()

	login := do(t, handler, ctx, http.MethodPost, "/api/auth/login/google", loginBody(), nil)
	if login.Code != http.StatusOK {
		t.Fatalf("login failed: %d (%s)", login.Code, login.Body.String())
	}
	var cookie *http.Cookie
	for _, c := range login.Result().Cookies() {
		if c.Name == refreshCookieName {
			cookie = c
		}
	}
	if cookie == nil {
		t.Fatalf("expected a refresh cookie")
	}

	rec := do(t, handler, ctx, http.MethodPost, "/api/auth/refresh", `{}`, func(r *http.Request) {
		r.AddCookie(cookie)
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected the cookie to be accepted, got %d (%s)", rec.Code, rec.Body.String())
	}
}

func TestAPIRefreshRequiresAToken(t *testing.T) {
	handler, ctx, done := newAPI(t, providerStub{})
	defer done()

	cases := []struct {
		name string
		body string
	}{
		{"empty body", ""},
		{"empty object", `{}`},
		{"blank token", `{"refresh_token":"   "}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := do(t, handler, ctx, http.MethodPost, "/api/auth/refresh", tc.body, nil)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d (%s)", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestAPILogoutRevokesTheRefreshToken(t *testing.T) {
	handler, ctx, done := newAPI(t, providerStub{profile: usersvc.SocialProfile{
		ProviderUserID: "g-api-5",
		Email:          "logout@example.com",
	}})
	defer done()

	login := do(t, handler, ctx, http.MethodPost, "/api/auth/login/google", loginBody(), nil)
	refresh := decodeData(t, login)["refresh_token"].(string)

	out := do(t, handler, ctx, http.MethodPost, "/api/auth/logout", `{"refresh_token":"`+refresh+`"}`, nil)
	if out.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d (%s)", out.Code, out.Body.String())
	}

	after := do(t, handler, ctx, http.MethodPost, "/api/auth/refresh", `{"refresh_token":"`+refresh+`"}`, nil)
	if after.Code != http.StatusUnauthorized {
		t.Fatalf("a revoked token must not rotate, got %d (%s)", after.Code, after.Body.String())
	}
}

func TestAPILogoutIsIdempotent(t *testing.T) {
	handler, ctx, done := newAPI(t, providerStub{})
	defer done()

	rec := do(t, handler, ctx, http.MethodPost, "/api/auth/logout", `{}`, nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("logging out without a token should succeed, got %d (%s)", rec.Code, rec.Body.String())
	}
}

func TestAPIMeRequiresAValidToken(t *testing.T) {
	handler, ctx, done := newAPI(t, providerStub{})
	defer done()

	cases := []struct {
		name   string
		header string
	}{
		{"no header", ""},
		{"garbage", "Bearer not-a-jwt"},
		{"wrong scheme", "Basic abc"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := do(t, handler, ctx, http.MethodGet, "/api/auth/me", "", func(r *http.Request) {
				if tc.header != "" {
					r.Header.Set("Authorization", tc.header)
				}
			})
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("expected 401, got %d (%s)", rec.Code, rec.Body.String())
			}
		})
	}
}
