package login

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/benpsk/go-starter/internal/users"
)

// Supported OAuth provider names.
const (
	ProviderGoogle = "google"
	ProviderGitHub = "github"
)

var (
	errVerifyUnauthorized = errors.New("oauth unauthorized")
	errVerifyInvalidInput = errors.New("oauth invalid input")
)

// SocialVerifier exchanges an authorization code for a verified profile. Tests
// substitute their own implementation instead of reaching a provider.
type SocialVerifier interface {
	ExchangeAndVerify(ctx context.Context, provider, code, codeVerifier, redirectURI string, cfg OAuthProviderConfig) (users.SocialProfile, error)
}

// providerEndpoints holds the URLs a verifier talks to. Tests point these at a
// local server; production uses providerDefaults.
type providerEndpoints struct {
	googleToken     string
	googleTokenInfo string
	githubToken     string
	githubUser      string
	githubEmails    string
}

func providerDefaults() providerEndpoints {
	return providerEndpoints{
		googleToken:     "https://oauth2.googleapis.com/token",
		googleTokenInfo: "https://oauth2.googleapis.com/tokeninfo",
		githubToken:     "https://github.com/login/oauth/access_token",
		githubUser:      "https://api.github.com/user",
		githubEmails:    "https://api.github.com/user/emails",
	}
}

type socialVerifier struct {
	httpClient *http.Client
	endpoints  providerEndpoints
}

// NewSocialVerifier builds the HTTP-backed provider verifier.
func NewSocialVerifier() SocialVerifier {
	return &socialVerifier{
		httpClient: &http.Client{Timeout: 10 * time.Second},
		endpoints:  providerDefaults(),
	}
}

// ExchangeAndVerify dispatches to the provider-specific exchange.
func (v *socialVerifier) ExchangeAndVerify(ctx context.Context, provider, code, codeVerifier, redirectURI string, cfg OAuthProviderConfig) (users.SocialProfile, error) {
	switch normalizeProvider(provider) {
	case ProviderGoogle:
		return v.google(ctx, code, codeVerifier, redirectURI, cfg)
	case ProviderGitHub:
		return v.github(ctx, code, codeVerifier, redirectURI, cfg)
	default:
		return users.SocialProfile{}, errVerifyInvalidInput
	}
}

func (v *socialVerifier) google(ctx context.Context, code, codeVerifier, redirectURI string, cfg OAuthProviderConfig) (users.SocialProfile, error) {
	if strings.TrimSpace(code) == "" || !cfg.Enabled() {
		return users.SocialProfile{}, errVerifyInvalidInput
	}

	values := url.Values{}
	values.Set("grant_type", "authorization_code")
	values.Set("code", code)
	values.Set("client_id", cfg.ClientID)
	values.Set("client_secret", cfg.ClientSecret)
	values.Set("redirect_uri", redirectURI)
	values.Set("code_verifier", codeVerifier)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, v.endpoints.googleToken, strings.NewReader(values.Encode()))
	if err != nil {
		return users.SocialProfile{}, errVerifyUnauthorized
	}
	req.Header.Set("content-type", "application/x-www-form-urlencoded")
	req.Header.Set("user-agent", userAgent)

	var tokenPayload struct {
		AccessToken string `json:"access_token"`
		IDToken     string `json:"id_token"`
	}
	status, err := v.doJSON(req, &tokenPayload)
	if err != nil || status < 200 || status >= 300 {
		return users.SocialProfile{}, errVerifyUnauthorized
	}

	params := url.Values{}
	if strings.TrimSpace(tokenPayload.IDToken) != "" {
		params.Set("id_token", strings.TrimSpace(tokenPayload.IDToken))
	} else {
		params.Set("access_token", strings.TrimSpace(tokenPayload.AccessToken))
	}
	if params.Get("id_token") == "" && params.Get("access_token") == "" {
		return users.SocialProfile{}, errVerifyUnauthorized
	}

	req, err = http.NewRequestWithContext(ctx, http.MethodGet, v.endpoints.googleTokenInfo+"?"+params.Encode(), nil)
	if err != nil {
		return users.SocialProfile{}, errVerifyUnauthorized
	}
	var payload struct {
		Sub           string `json:"sub"`
		Email         string `json:"email"`
		EmailVerified any    `json:"email_verified"`
		Name          string `json:"name"`
		Picture       string `json:"picture"`
		Aud           string `json:"aud"`
		Iss           string `json:"iss"`
	}
	status, err = v.doJSON(req, &payload)
	if err != nil || status != http.StatusOK {
		return users.SocialProfile{}, errVerifyUnauthorized
	}
	if strings.TrimSpace(payload.Sub) == "" || strings.TrimSpace(payload.Aud) != strings.TrimSpace(cfg.ClientID) {
		return users.SocialProfile{}, errVerifyUnauthorized
	}
	if payload.Iss != "accounts.google.com" && payload.Iss != "https://accounts.google.com" {
		return users.SocialProfile{}, errVerifyUnauthorized
	}
	return users.SocialProfile{
		Provider:       ProviderGoogle,
		ProviderUserID: strings.TrimSpace(payload.Sub),
		Email:          strings.TrimSpace(strings.ToLower(payload.Email)),
		EmailVerified:  parseTruthy(payload.EmailVerified),
		Name:           strings.TrimSpace(payload.Name),
		AvatarURL:      strings.TrimSpace(payload.Picture),
	}, nil
}

func (v *socialVerifier) github(ctx context.Context, code, codeVerifier, redirectURI string, cfg OAuthProviderConfig) (users.SocialProfile, error) {
	if strings.TrimSpace(code) == "" || !cfg.Enabled() {
		return users.SocialProfile{}, errVerifyInvalidInput
	}

	values := url.Values{}
	values.Set("client_id", cfg.ClientID)
	values.Set("client_secret", cfg.ClientSecret)
	values.Set("code", code)
	values.Set("redirect_uri", redirectURI)
	values.Set("code_verifier", codeVerifier)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, v.endpoints.githubToken, strings.NewReader(values.Encode()))
	if err != nil {
		return users.SocialProfile{}, errVerifyUnauthorized
	}
	req.Header.Set("accept", "application/json")
	req.Header.Set("content-type", "application/x-www-form-urlencoded")
	req.Header.Set("user-agent", userAgent)

	var tokenPayload struct {
		AccessToken string `json:"access_token"`
	}
	status, err := v.doJSON(req, &tokenPayload)
	if err != nil || status < 200 || status >= 300 || strings.TrimSpace(tokenPayload.AccessToken) == "" {
		return users.SocialProfile{}, errVerifyUnauthorized
	}

	headers := map[string]string{
		"Authorization": "Bearer " + strings.TrimSpace(tokenPayload.AccessToken),
		"Accept":        "application/vnd.github+json",
		"User-Agent":    userAgent,
	}

	req, err = http.NewRequestWithContext(ctx, http.MethodGet, v.endpoints.githubUser, nil)
	if err != nil {
		return users.SocialProfile{}, errVerifyUnauthorized
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	var ghUser struct {
		ID        int64  `json:"id"`
		Login     string `json:"login"`
		Name      string `json:"name"`
		Email     string `json:"email"`
		AvatarURL string `json:"avatar_url"`
	}
	status, err = v.doJSON(req, &ghUser)
	if err != nil || status != http.StatusOK || ghUser.ID <= 0 {
		return users.SocialProfile{}, errVerifyUnauthorized
	}

	email, emailVerified := v.githubPrimaryEmail(ctx, headers, ghUser.Email)
	name := strings.TrimSpace(ghUser.Name)
	if name == "" {
		name = strings.TrimSpace(ghUser.Login)
	}
	return users.SocialProfile{
		Provider:       ProviderGitHub,
		ProviderUserID: strconv.FormatInt(ghUser.ID, 10),
		Email:          email,
		EmailVerified:  emailVerified,
		Name:           name,
		AvatarURL:      strings.TrimSpace(ghUser.AvatarURL),
		Username:       strings.TrimSpace(ghUser.Login),
	}, nil
}

// githubPrimaryEmail prefers the verified primary address, then any verified
// address, and finally falls back to whatever the profile exposed.
func (v *socialVerifier) githubPrimaryEmail(ctx context.Context, headers map[string]string, profileEmail string) (string, bool) {
	email := strings.TrimSpace(strings.ToLower(profileEmail))
	verified := email != ""

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, v.endpoints.githubEmails, nil)
	if err != nil {
		return email, verified
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	var entries []struct {
		Email    string `json:"email"`
		Primary  bool   `json:"primary"`
		Verified bool   `json:"verified"`
	}
	status, err := v.doJSON(req, &entries)
	if err != nil || status != http.StatusOK {
		return email, verified
	}
	for _, entry := range entries {
		if entry.Primary && entry.Verified {
			return strings.TrimSpace(strings.ToLower(entry.Email)), true
		}
	}
	for _, entry := range entries {
		if entry.Verified {
			return strings.TrimSpace(strings.ToLower(entry.Email)), true
		}
	}
	return email, verified
}

func (v *socialVerifier) doJSON(req *http.Request, dst any) (int, error) {
	res, err := v.httpClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer res.Body.Close()
	if dst == nil {
		_, _ = io.Copy(io.Discard, res.Body)
		return res.StatusCode, nil
	}
	body, err := io.ReadAll(res.Body)
	if err != nil {
		return res.StatusCode, err
	}
	if len(body) == 0 {
		return res.StatusCode, nil
	}
	if err := json.Unmarshal(body, dst); err != nil {
		if res.StatusCode >= 200 && res.StatusCode < 300 {
			return res.StatusCode, fmt.Errorf("decode json: %w", err)
		}
		return res.StatusCode, nil
	}
	return res.StatusCode, nil
}

func parseTruthy(v any) bool {
	switch value := v.(type) {
	case bool:
		return value
	case string:
		value = strings.TrimSpace(strings.ToLower(value))
		return value == "true" || value == "1"
	default:
		return false
	}
}
