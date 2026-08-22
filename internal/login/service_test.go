package login

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/benpsk/go-starter/internal/apperror"
	"github.com/benpsk/go-starter/internal/users"
)

// tokenRepoStub is an in-memory Repository. The SQL behind the real one is
// covered by the integration tests in internal/postgres.
type tokenRepoStub struct {
	tokens        map[string]RefreshToken
	familyRevoked []string
	revoked       []string
	nextID        int64
}

func newTokenRepoStub() *tokenRepoStub {
	return &tokenRepoStub{tokens: map[string]RefreshToken{}, nextID: 1}
}

func (r *tokenRepoStub) CreateRefreshToken(_ context.Context, token RefreshToken) error {
	token.ID = r.nextID
	r.nextID++
	r.tokens[token.TokenHash] = token
	return nil
}

func (r *tokenRepoStub) RotateRefreshToken(_ context.Context, currentHash string, next RefreshToken, now time.Time) (RotationResult, error) {
	current, ok := r.tokens[currentHash]
	if !ok {
		return RotationResult{Authorized: false}, nil
	}
	if current.RevokedAt != nil || current.ReplacedByTokenID != nil || now.After(current.ExpiresAt) {
		return RotationResult{
			UserID:        current.UserID,
			FamilyID:      current.FamilyID,
			ReuseDetected: true,
			Authorized:    false,
		}, nil
	}

	next.ID = r.nextID
	r.nextID++
	next.UserID = current.UserID
	next.FamilyID = current.FamilyID
	r.tokens[next.TokenHash] = next

	replaced := next.ID
	current.RevokedAt = &now
	current.ReplacedByTokenID = &replaced
	r.tokens[currentHash] = current

	return RotationResult{UserID: current.UserID, FamilyID: current.FamilyID, Authorized: true}, nil
}

func (r *tokenRepoStub) RevokeRefreshTokenByHash(_ context.Context, tokenHash string, now time.Time) error {
	r.revoked = append(r.revoked, tokenHash)
	if token, ok := r.tokens[tokenHash]; ok {
		token.RevokedAt = &now
		r.tokens[tokenHash] = token
	}
	return nil
}

func (r *tokenRepoStub) RevokeRefreshTokenFamily(_ context.Context, familyID string, _ time.Time) error {
	r.familyRevoked = append(r.familyRevoked, familyID)
	return nil
}

// verifierStub returns a fixed profile instead of calling a provider.
type verifierStub struct {
	profile users.SocialProfile
	err     error
	calls   int
	lastURI string
}

func (v *verifierStub) ExchangeAndVerify(_ context.Context, provider, _, _, redirectURI string, _ OAuthProviderConfig) (users.SocialProfile, error) {
	v.calls++
	v.lastURI = redirectURI
	if v.err != nil {
		return users.SocialProfile{}, v.err
	}
	profile := v.profile
	if profile.Provider == "" {
		profile.Provider = provider
	}
	return profile, nil
}

// userServiceStub records the profile the login service resolved.
type userServiceStub struct {
	user users.User
	err  error
	seen users.SocialProfile
}

func (s *userServiceStub) FindByID(context.Context, int64) (users.User, error) {
	return s.user, s.err
}

func (s *userServiceStub) FindByEmail(context.Context, string) (users.User, error) {
	return s.user, s.err
}

func (s *userServiceStub) ListIdentities(context.Context, int64) ([]users.Identity, error) {
	return nil, s.err
}

func (s *userServiceStub) ResolveSocialProfile(_ context.Context, profile users.SocialProfile) (users.User, error) {
	s.seen = profile
	return s.user, s.err
}

func testConfig() Config {
	return Config{
		AppURL:            "https://app.example.com",
		AccessTokenSecret: "test-secret",
		AccessTokenTTL:    10 * time.Minute,
		RefreshTokenTTL:   24 * time.Hour,
		Google:            OAuthProviderConfig{ClientID: "google-id", ClientSecret: "google-secret"},
		GitHub:            OAuthProviderConfig{ClientID: "github-id", ClientSecret: "github-secret"},
	}
}

func newTestService(t *testing.T, cfg Config, verifier SocialVerifier, userSvc users.Service) (Service, *tokenRepoStub) {
	t.Helper()
	repo := newTokenRepoStub()
	return NewService(repo, userSvc, verifier, cfg), repo
}

func statusOf(t *testing.T, err error) int {
	t.Helper()
	var appErr *apperror.AppError
	if !errors.As(err, &appErr) {
		t.Fatalf("expected an *apperror.AppError, got %T: %v", err, err)
	}
	return appErr.Status
}

func TestProviderConfigAndEnabled(t *testing.T) {
	cfg := testConfig()
	cfg.GitHub = OAuthProviderConfig{ClientID: "only-id"}
	svc, _ := newTestService(t, cfg, &verifierStub{}, &userServiceStub{})

	cases := []struct {
		provider string
		known    bool
		enabled  bool
	}{
		{"google", true, true},
		{"GOOGLE", true, true},
		{"github", true, false}, // half-configured providers stay disabled
		{"apple", false, false},
	}
	for _, tc := range cases {
		t.Run(tc.provider, func(t *testing.T) {
			if _, ok := svc.ProviderConfig(tc.provider); ok != tc.known {
				t.Fatalf("ProviderConfig known = %v, want %v", ok, tc.known)
			}
			if got := svc.ProviderEnabled(tc.provider); got != tc.enabled {
				t.Fatalf("ProviderEnabled = %v, want %v", got, tc.enabled)
			}
		})
	}
}

func TestCallbackURL(t *testing.T) {
	svc, _ := newTestService(t, testConfig(), &verifierStub{}, &userServiceStub{})

	if got := svc.CallbackURL("Google"); got != "https://app.example.com/auth/callback/google" {
		t.Fatalf("unexpected callback url: %q", got)
	}
}

func TestStartOAuthBuildsPKCEAuthorizationURL(t *testing.T) {
	svc, _ := newTestService(t, testConfig(), &verifierStub{}, &userServiceStub{})

	start, err := svc.StartOAuth("google", "/account", time.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	parsed, err := url.Parse(start.AuthorizationURL)
	if err != nil {
		t.Fatalf("unparsable authorization url %q: %v", start.AuthorizationURL, err)
	}
	if parsed.Host != "accounts.google.com" {
		t.Fatalf("unexpected authorization host: %q", parsed.Host)
	}
	query := parsed.Query()
	if query.Get("state") != start.State || start.State == "" {
		t.Fatalf("expected the state to be carried into the url: %+v", start)
	}
	if query.Get("code_challenge_method") != "S256" || query.Get("code_challenge") == "" {
		t.Fatalf("expected a PKCE challenge: %v", query)
	}
	if query.Get("redirect_uri") != svc.CallbackURL("google") {
		t.Fatalf("unexpected redirect_uri: %q", query.Get("redirect_uri"))
	}
}

func TestStartOAuthRejectsUnconfiguredProvider(t *testing.T) {
	cfg := testConfig()
	cfg.Google = OAuthProviderConfig{}
	svc, _ := newTestService(t, cfg, &verifierStub{}, &userServiceStub{})

	_, err := svc.StartOAuth("google", "/account", time.Now())
	if statusOf(t, err) != http.StatusBadRequest {
		t.Fatalf("expected 400, got %v", err)
	}
	if !IsProviderNotConfigured(err) {
		t.Fatalf("expected the provider_not_configured code, got %v", err)
	}
}

func TestCompleteOAuthRoundTrip(t *testing.T) {
	verifier := &verifierStub{profile: users.SocialProfile{ProviderUserID: "g-1", Email: "a@example.com"}}
	userSvc := &userServiceStub{user: users.User{ID: 11, Email: "a@example.com"}}
	svc, _ := newTestService(t, testConfig(), verifier, userSvc)

	now := time.Now()
	start, err := svc.StartOAuth("google", "/dashboard", now)
	if err != nil {
		t.Fatalf("unexpected start error: %v", err)
	}

	completed, err := svc.CompleteOAuth(context.Background(), "google", start.State, "auth-code", now)
	if err != nil {
		t.Fatalf("unexpected complete error: %v", err)
	}
	if completed.User.ID != 11 {
		t.Fatalf("unexpected resolved user: %+v", completed.User)
	}
	if completed.RedirectTo != "/dashboard" {
		t.Fatalf("expected the stored redirect to survive, got %q", completed.RedirectTo)
	}
	if verifier.lastURI != svc.CallbackURL("google") {
		t.Fatalf("the exchange must reuse the registered redirect_uri, got %q", verifier.lastURI)
	}
	if userSvc.seen.ProviderUserID != "g-1" {
		t.Fatalf("expected the verified profile to be resolved, got %+v", userSvc.seen)
	}
}

func TestCompleteOAuthStateIsSingleUse(t *testing.T) {
	svc, _ := newTestService(t, testConfig(), &verifierStub{}, &userServiceStub{})

	now := time.Now()
	start, err := svc.StartOAuth("google", "/account", now)
	if err != nil {
		t.Fatalf("unexpected start error: %v", err)
	}
	if _, err := svc.CompleteOAuth(context.Background(), "google", start.State, "code", now); err != nil {
		t.Fatalf("first callback should succeed: %v", err)
	}
	if _, err := svc.CompleteOAuth(context.Background(), "google", start.State, "code", now); statusOf(t, err) != http.StatusUnauthorized {
		t.Fatalf("a replayed state must be rejected, got %v", err)
	}
}

func TestCompleteOAuthRejectsBadInput(t *testing.T) {
	svc, _ := newTestService(t, testConfig(), &verifierStub{}, &userServiceStub{})
	now := time.Now()
	start, _ := svc.StartOAuth("google", "/account", now)

	cases := []struct {
		name       string
		provider   string
		state      string
		code       string
		wantStatus int
	}{
		{"missing code", "google", start.State, "", http.StatusBadRequest},
		{"missing state", "google", "", "code", http.StatusBadRequest},
		{"unknown state", "google", "not-a-state", "code", http.StatusUnauthorized},
		{"provider mismatch", "github", start.State, "code", http.StatusUnauthorized},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := svc.CompleteOAuth(context.Background(), tc.provider, tc.state, tc.code, now)
			if got := statusOf(t, err); got != tc.wantStatus {
				t.Fatalf("expected %d, got %d (%v)", tc.wantStatus, got, err)
			}
		})
	}
}

func TestCompleteOAuthExpiredFlowIsRejected(t *testing.T) {
	cfg := testConfig()
	cfg.OAuthFlowTTL = time.Minute
	svc, _ := newTestService(t, cfg, &verifierStub{}, &userServiceStub{})

	now := time.Now()
	start, _ := svc.StartOAuth("google", "/account", now)

	_, err := svc.CompleteOAuth(context.Background(), "google", start.State, "code", now.Add(2*time.Minute))
	if statusOf(t, err) != http.StatusUnauthorized {
		t.Fatalf("expected an expired flow to be rejected, got %v", err)
	}
}

func TestExchangeCodeValidatesInput(t *testing.T) {
	svc, _ := newTestService(t, testConfig(), &verifierStub{}, &userServiceStub{user: users.User{ID: 1}})

	cases := []struct {
		name                     string
		code, verifier, redirect string
		wantStatus               int
	}{
		{"complete", "code", "verifier", "app://cb", 0},
		{"missing code", "", "verifier", "app://cb", http.StatusUnprocessableEntity},
		{"missing verifier", "code", "", "app://cb", http.StatusUnprocessableEntity},
		{"missing redirect", "code", "verifier", "", http.StatusUnprocessableEntity},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := svc.ExchangeCode(context.Background(), "google", tc.code, tc.verifier, tc.redirect)
			if tc.wantStatus == 0 {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if got := statusOf(t, err); got != tc.wantStatus {
				t.Fatalf("expected %d, got %d (%v)", tc.wantStatus, got, err)
			}
		})
	}
}

func TestExchangeCodeSurfacesVerificationFailure(t *testing.T) {
	svc, _ := newTestService(t, testConfig(), &verifierStub{err: errors.New("provider said no")}, &userServiceStub{})

	_, err := svc.ExchangeCode(context.Background(), "google", "code", "verifier", "app://cb")
	if statusOf(t, err) != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %v", err)
	}
}
