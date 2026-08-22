// Package login owns the authentication domain: OAuth authorization flows,
// social profile verification, and the API token pair lifecycle. It is
// transport-free and holds no database handle of its own.
package login

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"time"

	"github.com/benpsk/go-starter/internal/apperror"
	"github.com/benpsk/go-starter/internal/users"
)

// Service exposes the authentication operations both transports need.
type Service interface {
	// ProviderConfig returns the client credentials for a provider name.
	ProviderConfig(provider string) (OAuthProviderConfig, bool)
	// ProviderEnabled reports whether a provider is fully configured.
	ProviderEnabled(provider string) bool
	// APITokensConfigured reports whether JWT access tokens can be signed.
	APITokensConfigured() bool
	// CallbackURL is the redirect_uri this application registers with a provider.
	CallbackURL(provider string) string

	// StartOAuth creates a PKCE flow and returns where to send the browser.
	StartOAuth(provider, redirectTo string, now time.Time) (OAuthStart, error)
	// CompleteOAuth consumes a browser callback and resolves the account.
	CompleteOAuth(ctx context.Context, provider, state, code string, now time.Time) (CompletedLogin, error)
	// ExchangeCode completes a client-driven PKCE exchange (native clients).
	ExchangeCode(ctx context.Context, provider, code, codeVerifier, redirectURI string) (users.User, error)

	// IssueTokenPair mints a fresh access and refresh token for a user.
	IssueTokenPair(ctx context.Context, userID int64, now time.Time) (TokenPair, error)
	// RotateRefreshToken exchanges a refresh token for a new pair.
	RotateRefreshToken(ctx context.Context, refreshToken string, now time.Time) (TokenPair, error)
	// RevokeRefreshToken invalidates a single refresh token.
	RevokeRefreshToken(ctx context.Context, refreshToken string, now time.Time) error
	// ParseAccessToken validates a bearer token and returns its claims.
	ParseAccessToken(token string) (AccessTokenClaims, error)
	// PruneRefreshTokens removes refresh tokens that expired or were revoked
	// more than olderThan ago, and reports how many were removed.
	PruneRefreshTokens(ctx context.Context, olderThan time.Duration, now time.Time) (int64, error)
}

// Repository abstracts persistence for API refresh tokens.
type Repository interface {
	CreateRefreshToken(ctx context.Context, token RefreshToken) error
	RotateRefreshToken(ctx context.Context, currentHash string, next RefreshToken, now time.Time) (RotationResult, error)
	RevokeRefreshTokenByHash(ctx context.Context, tokenHash string, now time.Time) error
	RevokeRefreshTokenFamily(ctx context.Context, familyID string, now time.Time) error
	DeleteRefreshTokensBefore(ctx context.Context, cutoff time.Time) (int64, error)
}

// OAuthProviderConfig holds one provider's client credentials.
type OAuthProviderConfig struct {
	ClientID     string
	ClientSecret string
}

// Enabled reports whether both halves of the credential pair are present.
func (c OAuthProviderConfig) Enabled() bool {
	return strings.TrimSpace(c.ClientID) != "" && strings.TrimSpace(c.ClientSecret) != ""
}

// Config carries the runtime settings the login service depends on.
type Config struct {
	AppURL            string
	AccessTokenSecret string
	AccessTokenTTL    time.Duration
	RefreshTokenTTL   time.Duration
	OAuthFlowTTL      time.Duration
	Google            OAuthProviderConfig
	GitHub            OAuthProviderConfig
}

// OAuthStart describes a pending browser authorization redirect.
type OAuthStart struct {
	State            string
	AuthorizationURL string
}

// CompletedLogin is the outcome of a successful browser callback.
type CompletedLogin struct {
	User       users.User
	RedirectTo string
}

type service struct {
	repo     Repository
	users    users.Service
	verifier SocialVerifier
	flows    *oauthFlowStore
	cfg      Config
}

// NewService constructs a login service. The verifier is optional; nil selects
// the real HTTP-backed provider verifier.
func NewService(repo Repository, userService users.Service, verifier SocialVerifier, cfg Config) Service {
	if cfg.AccessTokenTTL <= 0 {
		cfg.AccessTokenTTL = 10 * time.Minute
	}
	if cfg.RefreshTokenTTL <= 0 {
		cfg.RefreshTokenTTL = 30 * 24 * time.Hour
	}
	if cfg.OAuthFlowTTL <= 0 {
		cfg.OAuthFlowTTL = 6 * time.Minute
	}
	if verifier == nil {
		verifier = NewSocialVerifier()
	}
	return &service{
		repo:     repo,
		users:    userService,
		verifier: verifier,
		flows:    newOAuthFlowStore(cfg.OAuthFlowTTL),
		cfg:      cfg,
	}
}

// ProviderConfig returns the credentials registered for a provider name.
func (s *service) ProviderConfig(provider string) (OAuthProviderConfig, bool) {
	switch normalizeProvider(provider) {
	case ProviderGoogle:
		return s.cfg.Google, true
	case ProviderGitHub:
		return s.cfg.GitHub, true
	default:
		return OAuthProviderConfig{}, false
	}
}

// ProviderEnabled reports whether a provider can be used for sign-in.
func (s *service) ProviderEnabled(provider string) bool {
	cfg, ok := s.ProviderConfig(provider)
	return ok && cfg.Enabled()
}

// APITokensConfigured reports whether an access token signing key is present.
func (s *service) APITokensConfigured() bool {
	return strings.TrimSpace(s.cfg.AccessTokenSecret) != ""
}

// CallbackURL builds the absolute redirect_uri for a provider.
func (s *service) CallbackURL(provider string) string {
	base := strings.TrimRight(strings.TrimSpace(s.cfg.AppURL), "/")
	return base + "/auth/callback/" + normalizeProvider(provider)
}

// StartOAuth records a PKCE flow and returns the provider authorization URL.
func (s *service) StartOAuth(provider, redirectTo string, now time.Time) (OAuthStart, error) {
	provider = normalizeProvider(provider)
	cfg, ok := s.ProviderConfig(provider)
	if !ok || !cfg.Enabled() {
		return OAuthStart{}, providerNotConfigured()
	}
	flow, err := s.flows.create(provider, redirectTo, now)
	if err != nil {
		return OAuthStart{}, apperror.Internal("failed to start oauth flow", err)
	}
	return OAuthStart{
		State:            flow.State,
		AuthorizationURL: s.authorizationURL(provider, cfg, flow),
	}, nil
}

// CompleteOAuth validates the callback state, exchanges the code, and resolves
// the account the verified profile belongs to.
func (s *service) CompleteOAuth(ctx context.Context, provider, state, code string, now time.Time) (CompletedLogin, error) {
	provider = normalizeProvider(provider)
	if strings.TrimSpace(code) == "" || strings.TrimSpace(state) == "" {
		return CompletedLogin{}, apperror.BadRequest("code and state are required")
	}
	cfg, ok := s.ProviderConfig(provider)
	if !ok || !cfg.Enabled() {
		return CompletedLogin{}, providerNotConfigured()
	}
	flow, err := s.flows.consume(state, provider, now)
	if err != nil {
		return CompletedLogin{}, apperror.Unauthorized("oauth state is invalid or expired")
	}
	profile, err := s.verifier.ExchangeAndVerify(ctx, provider, code, flow.CodeVerifier, s.CallbackURL(provider), cfg)
	if err != nil {
		return CompletedLogin{}, apperror.Unauthorized("oauth verification failed")
	}
	resolved, err := s.users.ResolveSocialProfile(ctx, profile)
	if err != nil {
		return CompletedLogin{}, err
	}
	return CompletedLogin{User: resolved, RedirectTo: flow.RedirectTo}, nil
}

// ExchangeCode completes a PKCE exchange the client itself started, which is
// how native clients sign in.
func (s *service) ExchangeCode(ctx context.Context, provider, code, codeVerifier, redirectURI string) (users.User, error) {
	provider = normalizeProvider(provider)
	cfg, ok := s.ProviderConfig(provider)
	if !ok || !cfg.Enabled() {
		return users.User{}, providerNotConfigured()
	}
	if strings.TrimSpace(code) == "" || strings.TrimSpace(codeVerifier) == "" || strings.TrimSpace(redirectURI) == "" {
		return users.User{}, apperror.Validation("invalid login request", map[string]string{
			"code":          "required",
			"code_verifier": "required",
			"redirect_uri":  "required",
		})
	}
	profile, err := s.verifier.ExchangeAndVerify(ctx, provider, code, codeVerifier, strings.TrimSpace(redirectURI), cfg)
	if err != nil {
		return users.User{}, apperror.Unauthorized("oauth login failed")
	}
	return s.users.ResolveSocialProfile(ctx, profile)
}

func (s *service) authorizationURL(provider string, cfg OAuthProviderConfig, flow oauthFlow) string {
	q := url.Values{}
	q.Set("client_id", cfg.ClientID)
	q.Set("redirect_uri", s.CallbackURL(provider))
	q.Set("state", flow.State)
	q.Set("code_challenge", codeChallenge(flow.CodeVerifier))
	q.Set("code_challenge_method", "S256")

	switch provider {
	case ProviderGoogle:
		q.Set("response_type", "code")
		q.Set("scope", "openid email profile")
		return "https://accounts.google.com/o/oauth2/v2/auth?" + q.Encode()
	case ProviderGitHub:
		q.Set("scope", "read:user user:email")
		return "https://github.com/login/oauth/authorize?" + q.Encode()
	default:
		return "/auth/login?error=provider_not_configured"
	}
}

func providerNotConfigured() error {
	return apperror.WithCode(
		apperror.BadRequest("provider is not configured"),
		"provider_not_configured",
	)
}

func normalizeProvider(provider string) string {
	return strings.TrimSpace(strings.ToLower(provider))
}

// IsProviderNotConfigured reports whether an error came from an unconfigured
// provider, so browser handlers can pick the right redirect message.
func IsProviderNotConfigured(err error) bool {
	var appErr *apperror.AppError
	return errors.As(err, &appErr) && appErr.Code == "provider_not_configured"
}
