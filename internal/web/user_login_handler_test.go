package web_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/benpsk/go-starter/internal/appsvc"
	usersession "github.com/benpsk/go-starter/internal/auth/user"
	"github.com/benpsk/go-starter/internal/config"
	loginsvc "github.com/benpsk/go-starter/internal/login"
	"github.com/benpsk/go-starter/internal/postgres"
	"github.com/benpsk/go-starter/internal/storage"
	"github.com/benpsk/go-starter/internal/testutil"
	usersvc "github.com/benpsk/go-starter/internal/users"
	"github.com/benpsk/go-starter/internal/web"
	"github.com/go-chi/chi/v5"
)

// These are feature tests: the real browser router, real services, and a real
// database. Only the OAuth provider is substituted.

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

func webTestConfig() config.Config {
	return config.Config{
		AppName: "Go Starter",
		AppEnv:  "test",
		AppURL:  "http://127.0.0.1:8080",
		Auth: config.AuthConfig{
			SessionCookieName: "go_starter_session",
			SessionTTL:        time.Hour,
			Social: config.SocialAuthConfig{
				Google: config.OAuthClientConfig{ClientID: "google-id", ClientSecret: "google-secret"},
			},
		},
		RateLimit: config.RateLimitConfig{Window: time.Minute, APILogin: 100, APIRefresh: 100, WebOAuth: 100},
	}
}

// browser mounts the web routes over the test database and keeps the cookie
// jar a real browser would keep.
type browser struct {
	handler http.Handler
	ctx     context.Context
	cookies map[string]*http.Cookie
	t       *testing.T
}

func newBrowser(t *testing.T, verifier loginsvc.SocialVerifier) (*browser, func()) {
	t.Helper()

	pool := testutil.SetupDB(t)
	ctx, tx, done := testutil.BeginTx(t, pool)

	cfg := webTestConfig()
	userService := usersvc.NewService(postgres.NewUserRepository(pool))
	loginService := loginsvc.NewService(
		postgres.NewLoginRepository(pool),
		userService,
		verifier,
		loginsvc.Config{
			AppURL: cfg.AppURL,
			Google: loginsvc.OAuthProviderConfig{
				ClientID:     cfg.Auth.Social.Google.ClientID,
				ClientSecret: cfg.Auth.Social.Google.ClientSecret,
			},
		},
	)

	// The session manager reads and writes outside the request context, so it
	// is given the transaction directly to keep the test isolated.
	sessions := usersession.NewManager(tx, cfg.Auth.SessionCookieName, cfg.Auth.SessionTTL, false)

	r := chi.NewRouter()
	web.Mount(r, cfg, sessions, appsvc.Services{
		Users:   userService,
		Login:   loginService,
		Storage: storage.NewDisabledService("not configured in tests"),
	})

	return &browser{handler: r, ctx: ctx, cookies: map[string]*http.Cookie{}, t: t}, done
}

func (b *browser) get(path string) *httptest.ResponseRecorder {
	return b.send(http.MethodGet, path, nil)
}

// post submits a form the way the page would, including the CSRF token app.js
// injects from the cookie.
func (b *browser) post(path string, form url.Values) *httptest.ResponseRecorder {
	if form == nil {
		form = url.Values{}
	}
	if token, ok := b.cookies["csrf_token"]; ok {
		form.Set("csrf_token", token.Value)
	}
	return b.send(http.MethodPost, path, form)
}

func (b *browser) send(method, path string, form url.Values) *httptest.ResponseRecorder {
	b.t.Helper()

	var req *http.Request
	if form == nil {
		req = httptest.NewRequest(method, path, nil)
	} else {
		req = httptest.NewRequest(method, path, strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	req = req.WithContext(b.ctx)
	for _, cookie := range b.cookies {
		req.AddCookie(cookie)
	}

	rec := httptest.NewRecorder()
	b.handler.ServeHTTP(rec, req)
	for _, cookie := range rec.Result().Cookies() {
		if cookie.MaxAge < 0 {
			delete(b.cookies, cookie.Name)
			continue
		}
		b.cookies[cookie.Name] = cookie
	}
	return rec
}

func TestBrowserLoginFlow(t *testing.T) {
	client, done := newBrowser(t, providerStub{profile: usersvc.SocialProfile{
		ProviderUserID: "g-web-1",
		Email:          "web@example.com",
		EmailVerified:  true,
		Name:           "Web User",
		AvatarURL:      "https://img.example.com/web.png",
	}})
	defer done()

	// The login page seeds the CSRF cookie the form post needs.
	page := client.get("/auth/login")
	if page.Code != http.StatusOK {
		t.Fatalf("expected the login page, got %d", page.Code)
	}
	if !strings.Contains(page.Body.String(), "Continue with Google") {
		t.Fatalf("expected the configured provider to be offered")
	}

	start := client.post("/auth/login/google", url.Values{"next": {"/account"}})
	if start.Code != http.StatusSeeOther {
		t.Fatalf("expected a redirect to the provider, got %d (%s)", start.Code, start.Body.String())
	}
	authorizeURL, err := url.Parse(start.Header().Get("Location"))
	if err != nil {
		t.Fatalf("unparsable authorization url: %v", err)
	}
	if authorizeURL.Host != "accounts.google.com" {
		t.Fatalf("unexpected authorization host %q", authorizeURL.Host)
	}
	state := authorizeURL.Query().Get("state")
	if state == "" {
		t.Fatalf("expected a state parameter")
	}

	callback := client.get("/auth/callback/google?code=auth-code&state=" + url.QueryEscape(state))
	if callback.Code != http.StatusSeeOther {
		t.Fatalf("expected a redirect after the callback, got %d (%s)", callback.Code, callback.Body.String())
	}
	if got := callback.Header().Get("Location"); got != "/account" {
		t.Fatalf("expected to land on the requested page, got %q", got)
	}
	sessionCookie, ok := client.cookies["go_starter_session"]
	if !ok {
		t.Fatalf("expected a session cookie to be issued")
	}
	if !sessionCookie.HttpOnly {
		t.Fatalf("the session cookie must be HttpOnly: %+v", sessionCookie)
	}

	t.Run("the account page renders the signed-in user", func(t *testing.T) {
		account := client.get("/account")
		if account.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", account.Code)
		}
		body := account.Body.String()
		if !strings.Contains(body, "Web User") || !strings.Contains(body, "web@example.com") {
			t.Fatalf("expected the profile to be rendered: %s", body)
		}
		if !strings.Contains(body, "google") {
			t.Fatalf("expected the linked provider to be listed")
		}
	})

	t.Run("the header shows the session", func(t *testing.T) {
		home := client.get("/")
		if !strings.Contains(home.Body.String(), "Logout") {
			t.Fatalf("expected the header to offer logout when signed in")
		}
	})

	t.Run("signing in again is blocked for authenticated visitors", func(t *testing.T) {
		again := client.get("/auth/login")
		if again.Code != http.StatusSeeOther {
			t.Fatalf("expected a redirect away from the login page, got %d", again.Code)
		}
		if got := again.Header().Get("Location"); got != "/account" {
			t.Fatalf("unexpected redirect target %q", got)
		}
	})

	t.Run("logout clears the session", func(t *testing.T) {
		out := client.post("/auth/logout", nil)
		if out.Code != http.StatusSeeOther {
			t.Fatalf("expected a redirect, got %d", out.Code)
		}
		if _, still := client.cookies["go_starter_session"]; still {
			t.Fatalf("expected the session cookie to be cleared")
		}
		after := client.get("/account")
		if after.Code != http.StatusSeeOther {
			t.Fatalf("the account page must require a session again, got %d", after.Code)
		}
	})
}

func TestBrowserCallbackRejectsReplayedState(t *testing.T) {
	client, done := newBrowser(t, providerStub{profile: usersvc.SocialProfile{
		ProviderUserID: "g-web-2",
		Email:          "replay@example.com",
	}})
	defer done()

	client.get("/auth/login")
	start := client.post("/auth/login/google", nil)
	state := mustState(t, start.Header().Get("Location"))

	first := client.get("/auth/callback/google?code=c&state=" + url.QueryEscape(state))
	if got := first.Header().Get("Location"); got != "/account" {
		t.Fatalf("the first callback should succeed, got %d to %q", first.Code, got)
	}

	// Drop the session the first callback issued, so the replay is judged on
	// the state alone rather than being turned away as already signed in.
	delete(client.cookies, "go_starter_session")

	second := client.get("/auth/callback/google?code=c&state=" + url.QueryEscape(state))
	if got := second.Header().Get("Location"); !strings.Contains(got, "error=oauth_failed") {
		t.Fatalf("a replayed state must fail, got %q", got)
	}
}

func TestBrowserCallbackFailureModes(t *testing.T) {
	cases := []struct {
		name      string
		verifier  loginsvc.SocialVerifier
		query     string
		wantError string
	}{
		{
			name:      "provider returned an error",
			verifier:  providerStub{},
			query:     "?error=access_denied",
			wantError: "error=oauth_failed",
		},
		{
			name:      "missing code",
			verifier:  providerStub{},
			query:     "?state=abc",
			wantError: "error=oauth_failed",
		},
		{
			name:      "unknown state",
			verifier:  providerStub{},
			query:     "?code=c&state=not-a-state",
			wantError: "error=oauth_failed",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client, done := newBrowser(t, tc.verifier)
			defer done()

			rec := client.get("/auth/callback/google" + tc.query)
			if rec.Code != http.StatusSeeOther {
				t.Fatalf("expected a redirect, got %d", rec.Code)
			}
			if got := rec.Header().Get("Location"); !strings.Contains(got, tc.wantError) {
				t.Fatalf("expected %q in %q", tc.wantError, got)
			}
		})
	}
}

func TestBrowserLoginRejectsUnconfiguredProvider(t *testing.T) {
	client, done := newBrowser(t, providerStub{})
	defer done()

	client.get("/auth/login")
	rec := client.post("/auth/login/github", nil)

	if got := rec.Header().Get("Location"); !strings.Contains(got, "error=provider_not_configured") {
		t.Fatalf("expected a provider_not_configured redirect, got %q", got)
	}
}

func TestBrowserLoginSurfacesAccountConflict(t *testing.T) {
	client, done := newBrowser(t, providerStub{profile: usersvc.SocialProfile{
		ProviderUserID: "g-web-3",
		Email:          "conflict@example.com",
		EmailVerified:  true,
	}})
	defer done()

	// Take the email with a different provider first, so the Google sign-in
	// collides on a verified address.
	if _, err := postgres.NewUserRepository(testutil.SetupDB(t)).CreateWithIdentity(client.ctx, usersvc.SocialProfile{
		Provider:       "github",
		ProviderUserID: "gh-web-3",
		Email:          "conflict@example.com",
		Name:           "Existing",
	}); err != nil {
		t.Fatalf("seed conflicting user: %v", err)
	}

	client.get("/auth/login")
	start := client.post("/auth/login/google", nil)
	state := mustState(t, start.Header().Get("Location"))

	rec := client.get("/auth/callback/google?code=c&state=" + url.QueryEscape(state))
	if got := rec.Header().Get("Location"); !strings.Contains(got, "error=account_conflict") {
		t.Fatalf("expected an account_conflict redirect, got %q", got)
	}

	t.Run("the page explains the conflict", func(t *testing.T) {
		page := client.get("/auth/login?error=account_conflict")
		if !strings.Contains(page.Body.String(), "already exists under another provider") {
			t.Fatalf("expected the conflict message to be rendered")
		}
	})
}

func TestBrowserLoginRedirectTargetIsRestricted(t *testing.T) {
	cases := []struct {
		name string
		next string
	}{
		{"absolute url", "https://evil.example.com/steal"},
		{"protocol relative", "//evil.example.com/steal"},
		{"empty", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client, done := newBrowser(t, providerStub{profile: usersvc.SocialProfile{
				ProviderUserID: "g-web-redirect-" + tc.name,
				Email:          "redirect-" + strings.ReplaceAll(tc.name, " ", "-") + "@example.com",
			}})
			defer done()

			client.get("/auth/login")
			start := client.post("/auth/login/google", url.Values{"next": {tc.next}})
			state := mustState(t, start.Header().Get("Location"))

			rec := client.get("/auth/callback/google?code=c&state=" + url.QueryEscape(state))
			if got := rec.Header().Get("Location"); got != "/account" {
				t.Fatalf("an off-site redirect must be refused, got %q", got)
			}
		})
	}
}

func mustState(t *testing.T, location string) string {
	t.Helper()
	parsed, err := url.Parse(location)
	if err != nil {
		t.Fatalf("unparsable location %q: %v", location, err)
	}
	state := parsed.Query().Get("state")
	if state == "" {
		t.Fatalf("no state in %q", location)
	}
	return state
}
