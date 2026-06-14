package auth

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"time"

	"github.com/benpsk/go-starter/internal/config"
	"github.com/benpsk/go-starter/internal/postgres"
	"github.com/benpsk/go-starter/internal/user"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Service struct {
	users                    *postgres.UserAuthStore
	appEnv                   string
	appURL                   string
	sessionCookieName        string
	sessionTTL               time.Duration
	sessionCookieForceSecure bool
	apiAccessTokenSecret     string
	apiAccessTokenTTL        time.Duration
	apiRefreshTokenTTL       time.Duration
	apiRefreshCookieName     string
	oauthFlows               *oauthFlowStore
	verifier                 SocialVerifier
	googleOAuth              ProviderConfig
	githubOAuth              ProviderConfig
}

func NewService(db *pgxpool.Pool, cfg config.Config) *Service {
	return &Service{
		users:                    postgres.NewUserAuthStore(db),
		appEnv:                   cfg.AppEnv,
		appURL:                   cfg.AppURL,
		sessionCookieName:        cfg.Auth.SessionCookieName,
		sessionTTL:               cfg.Auth.SessionTTL,
		sessionCookieForceSecure: cfg.Auth.CookieSecure,
		apiAccessTokenSecret:     cfg.Auth.API.AccessTokenSecret,
		apiAccessTokenTTL:        cfg.Auth.API.AccessTokenTTL,
		apiRefreshTokenTTL:       cfg.Auth.API.RefreshTokenTTL,
		apiRefreshCookieName:     cfg.Auth.API.RefreshCookieName,
		oauthFlows:               newOAuthFlowStore(6 * time.Minute),
		verifier:                 NewSocialVerifier(),
		googleOAuth: ProviderConfig{
			ClientID:     cfg.Auth.Social.Google.ClientID,
			ClientSecret: cfg.Auth.Social.Google.ClientSecret,
		},
		githubOAuth: ProviderConfig{
			ClientID:     cfg.Auth.Social.GitHub.ClientID,
			ClientSecret: cfg.Auth.Social.GitHub.ClientSecret,
		},
	}
}

func (s *Service) Users() *postgres.UserAuthStore {
	return s.users
}

func (s *Service) SetVerifier(verifier SocialVerifier) {
	if verifier != nil {
		s.verifier = verifier
	}
}

func (s *Service) APIRefreshCookieName() string {
	return s.apiRefreshCookieName
}

func (s *Service) SessionCookieName() string {
	return s.sessionCookieName
}

func (s *Service) APIAuthConfigured() bool {
	return strings.TrimSpace(s.apiAccessTokenSecret) != ""
}

func (s *Service) ProviderConfig(provider string) (ProviderConfig, bool) {
	switch strings.TrimSpace(strings.ToLower(provider)) {
	case "google":
		return s.googleOAuth, true
	case "github":
		return s.githubOAuth, true
	default:
		return ProviderConfig{}, false
	}
}

func ProviderEnabled(cfg ProviderConfig) bool {
	return cfg.ClientID != "" && cfg.ClientSecret != ""
}

func (s *Service) OAuthCallbackURL(provider string) string {
	base := strings.TrimRight(strings.TrimSpace(s.appURL), "/")
	return base + "/auth/callback/" + strings.TrimSpace(strings.ToLower(provider))
}

func (s *Service) OAuthAuthorizationURL(provider string, cfg ProviderConfig, flow OAuthFlowRecord) string {
	redirectURI := s.OAuthCallbackURL(provider)
	challenge := oauthCodeChallenge(flow.CodeVerifier)

	switch provider {
	case "google":
		q := url.Values{}
		q.Set("client_id", cfg.ClientID)
		q.Set("redirect_uri", redirectURI)
		q.Set("response_type", "code")
		q.Set("scope", "openid email profile")
		q.Set("state", flow.State)
		q.Set("code_challenge", challenge)
		q.Set("code_challenge_method", "S256")
		return "https://accounts.google.com/o/oauth2/v2/auth?" + q.Encode()
	case "github":
		q := url.Values{}
		q.Set("client_id", cfg.ClientID)
		q.Set("redirect_uri", redirectURI)
		q.Set("scope", "read:user user:email")
		q.Set("state", flow.State)
		q.Set("code_challenge", challenge)
		q.Set("code_challenge_method", "S256")
		return "https://github.com/login/oauth/authorize?" + q.Encode()
	default:
		return "/auth/login?error=oauth_failed"
	}
}

func (s *Service) CreateOAuthFlow(provider, redirectTo string, now time.Time) (OAuthFlowRecord, error) {
	return s.oauthFlows.create(provider, redirectTo, now)
}

func (s *Service) ConsumeOAuthFlow(state, provider string, now time.Time) (OAuthFlowRecord, error) {
	return s.oauthFlows.consume(state, provider, now)
}

func (s *Service) ExchangeAndVerify(ctx context.Context, provider string, code string, codeVerifier string, redirectURI string, cfg ProviderConfig) (user.SocialProfile, error) {
	return s.verifier.ExchangeAndVerify(ctx, provider, code, codeVerifier, redirectURI, cfg)
}

func (s *Service) FindOrCreateSocialUser(ctx context.Context, profile user.SocialProfile) (user.User, error) {
	currentUser, err := s.users.FindByIdentity(ctx, profile.Provider, profile.ProviderUserID)
	if err == nil {
		_ = s.users.UpdateUserFromProfile(ctx, currentUser.ID, profile)
		return s.users.FindByID(ctx, currentUser.ID)
	}
	if err != nil && !errors.Is(err, user.ErrNotFound) {
		return user.User{}, err
	}

	if profile.EmailVerified && strings.TrimSpace(profile.Email) != "" {
		if _, err := s.users.FindByEmail(ctx, profile.Email); err == nil {
			return user.User{}, user.ErrEmailConflict
		}
	}
	return s.users.CreateUserWithIdentity(ctx, profile)
}
