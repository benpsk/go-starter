package server

import (
	"context"
	"errors"
	"net/url"
	"strings"

	"github.com/benpsk/go-starter/internal/user"
)

func (h handler) oauthProviderConfig(provider string) (oauthProviderConfig, bool) {
	switch strings.TrimSpace(strings.ToLower(provider)) {
	case "google":
		return h.googleOAuth, true
	case "github":
		return h.githubOAuth, true
	default:
		return oauthProviderConfig{}, false
	}
}

func (h handler) oauthCallbackURL(provider string) string {
	base := strings.TrimRight(strings.TrimSpace(h.appURL), "/")
	return base + "/auth/callback/" + strings.TrimSpace(strings.ToLower(provider))
}

func (h handler) oauthAuthorizationURL(provider string, cfg oauthProviderConfig, flow oauthFlowRecord) string {
	redirectURI := h.oauthCallbackURL(provider)
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

func (h handler) findOrCreateSocialUser(ctx context.Context, profile user.SocialProfile) (user.User, error) {
	currentUser, err := h.users.FindByIdentity(ctx, profile.Provider, profile.ProviderUserID)
	if err == nil {
		_ = h.users.UpdateUserFromProfile(ctx, currentUser.ID, profile)
		return h.users.FindByID(ctx, currentUser.ID)
	}
	if err != nil && !errors.Is(err, user.ErrNotFound) {
		return user.User{}, err
	}

	if profile.EmailVerified && strings.TrimSpace(profile.Email) != "" {
		if _, err := h.users.FindByEmail(ctx, profile.Email); err == nil {
			return user.User{}, user.ErrEmailConflict
		}
	}
	return h.users.CreateUserWithIdentity(ctx, profile)
}
