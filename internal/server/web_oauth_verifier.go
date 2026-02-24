package server

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/benpsk/go-starter/internal/user"
)

var (
	errOAuthUnauthorized = errors.New("oauth unauthorized")
	errOAuthInvalidInput = errors.New("oauth invalid input")
)

type socialVerifier struct {
	httpClient *http.Client
}

type socialAuthVerifier interface {
	ExchangeAndVerify(ctx context.Context, provider string, code string, codeVerifier string, redirectURI string, cfg oauthProviderConfig) (user.SocialProfile, error)
}

func newSocialVerifier() *socialVerifier {
	return &socialVerifier{httpClient: &http.Client{Timeout: 10 * time.Second}}
}

func (v *socialVerifier) ExchangeAndVerify(ctx context.Context, provider string, code string, codeVerifier string, redirectURI string, cfg oauthProviderConfig) (user.SocialProfile, error) {
	provider = strings.TrimSpace(strings.ToLower(provider))
	switch provider {
	case "google":
		return v.google(ctx, code, codeVerifier, redirectURI, cfg)
	case "github":
		return v.github(ctx, code, codeVerifier, redirectURI, cfg)
	default:
		return user.SocialProfile{}, errOAuthInvalidInput
	}
}

type oauthProviderConfig struct {
	ClientID     string
	ClientSecret string
}

func (v *socialVerifier) google(ctx context.Context, code, codeVerifier, redirectURI string, cfg oauthProviderConfig) (user.SocialProfile, error) {
	if strings.TrimSpace(code) == "" || strings.TrimSpace(cfg.ClientID) == "" || strings.TrimSpace(cfg.ClientSecret) == "" {
		return user.SocialProfile{}, errOAuthInvalidInput
	}

	values := url.Values{}
	values.Set("grant_type", "authorization_code")
	values.Set("code", code)
	values.Set("client_id", cfg.ClientID)
	values.Set("client_secret", cfg.ClientSecret)
	values.Set("redirect_uri", redirectURI)
	values.Set("code_verifier", codeVerifier)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://oauth2.googleapis.com/token", strings.NewReader(values.Encode()))
	if err != nil {
		return user.SocialProfile{}, errOAuthUnauthorized
	}
	req.Header.Set("content-type", "application/x-www-form-urlencoded")
	req.Header.Set("user-agent", "go-starter")

	var tokenPayload struct {
		AccessToken string `json:"access_token"`
		IDToken     string `json:"id_token"`
	}
	status, err := v.doJSON(req, &tokenPayload)
	if err != nil || status < 200 || status >= 300 {
		return user.SocialProfile{}, errOAuthUnauthorized
	}

	params := url.Values{}
	if strings.TrimSpace(tokenPayload.IDToken) != "" {
		params.Set("id_token", strings.TrimSpace(tokenPayload.IDToken))
	} else {
		params.Set("access_token", strings.TrimSpace(tokenPayload.AccessToken))
	}
	if params.Get("id_token") == "" && params.Get("access_token") == "" {
		return user.SocialProfile{}, errOAuthUnauthorized
	}

	req, err = http.NewRequestWithContext(ctx, http.MethodGet, "https://oauth2.googleapis.com/tokeninfo?"+params.Encode(), nil)
	if err != nil {
		return user.SocialProfile{}, errOAuthUnauthorized
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
		return user.SocialProfile{}, errOAuthUnauthorized
	}
	if strings.TrimSpace(payload.Sub) == "" || strings.TrimSpace(payload.Aud) != strings.TrimSpace(cfg.ClientID) {
		return user.SocialProfile{}, errOAuthUnauthorized
	}
	if payload.Iss != "accounts.google.com" && payload.Iss != "https://accounts.google.com" {
		return user.SocialProfile{}, errOAuthUnauthorized
	}
	return user.SocialProfile{
		Provider:       "google",
		ProviderUserID: strings.TrimSpace(payload.Sub),
		Email:          strings.TrimSpace(strings.ToLower(payload.Email)),
		EmailVerified:  parseTruthy(payload.EmailVerified),
		Name:           strings.TrimSpace(payload.Name),
		AvatarURL:      strings.TrimSpace(payload.Picture),
	}, nil
}

func (v *socialVerifier) github(ctx context.Context, code, codeVerifier, redirectURI string, cfg oauthProviderConfig) (user.SocialProfile, error) {
	if strings.TrimSpace(code) == "" || strings.TrimSpace(cfg.ClientID) == "" || strings.TrimSpace(cfg.ClientSecret) == "" {
		return user.SocialProfile{}, errOAuthInvalidInput
	}

	values := url.Values{}
	values.Set("client_id", cfg.ClientID)
	values.Set("client_secret", cfg.ClientSecret)
	values.Set("code", code)
	values.Set("redirect_uri", redirectURI)
	values.Set("code_verifier", codeVerifier)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://github.com/login/oauth/access_token", strings.NewReader(values.Encode()))
	if err != nil {
		return user.SocialProfile{}, errOAuthUnauthorized
	}
	req.Header.Set("accept", "application/json")
	req.Header.Set("content-type", "application/x-www-form-urlencoded")
	req.Header.Set("user-agent", "go-starter")

	var tokenPayload struct {
		AccessToken string `json:"access_token"`
	}
	status, err := v.doJSON(req, &tokenPayload)
	if err != nil || status < 200 || status >= 300 || strings.TrimSpace(tokenPayload.AccessToken) == "" {
		return user.SocialProfile{}, errOAuthUnauthorized
	}

	headers := map[string]string{
		"Authorization": "Bearer " + strings.TrimSpace(tokenPayload.AccessToken),
		"Accept":        "application/vnd.github+json",
		"User-Agent":    "go-starter",
	}
	req, _ = http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/user", nil)
	for k, val := range headers {
		req.Header.Set(k, val)
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
		return user.SocialProfile{}, errOAuthUnauthorized
	}

	req, _ = http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/user/emails", nil)
	for k, val := range headers {
		req.Header.Set(k, val)
	}
	var emails []struct {
		Email    string `json:"email"`
		Primary  bool   `json:"primary"`
		Verified bool   `json:"verified"`
	}
	status, err = v.doJSON(req, &emails)
	email := strings.TrimSpace(strings.ToLower(ghUser.Email))
	emailVerified := email != ""
	if err == nil && status == http.StatusOK {
		for _, item := range emails {
			if item.Primary && item.Verified {
				email = strings.TrimSpace(strings.ToLower(item.Email))
				emailVerified = true
				break
			}
		}
		if !emailVerified {
			for _, item := range emails {
				if item.Verified {
					email = strings.TrimSpace(strings.ToLower(item.Email))
					emailVerified = true
					break
				}
			}
		}
	}
	name := strings.TrimSpace(ghUser.Name)
	if name == "" {
		name = strings.TrimSpace(ghUser.Login)
	}
	return user.SocialProfile{
		Provider:       "github",
		ProviderUserID: strconv.FormatInt(ghUser.ID, 10),
		Email:          email,
		EmailVerified:  emailVerified,
		Name:           name,
		AvatarURL:      strings.TrimSpace(ghUser.AvatarURL),
		Username:       strings.TrimSpace(ghUser.Login),
	}, nil
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

func oauthCodeChallenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
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
