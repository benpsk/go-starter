package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/benpsk/go-starter/internal/config"
	"github.com/benpsk/go-starter/internal/user"
	"github.com/go-chi/chi/v5"
)

type fakeSocialVerifier struct {
	profile user.SocialProfile
	err     error
}

func (f fakeSocialVerifier) ExchangeAndVerify(ctx context.Context, provider string, code string, codeVerifier string, redirectURI string, cfg oauthProviderConfig) (user.SocialProfile, error) {
	if f.err != nil {
		return user.SocialProfile{}, f.err
	}
	p := f.profile
	if p.Provider == "" {
		p.Provider = provider
	}
	return p, nil
}

func TestAPILoginIssuesTokensAndSetsRefreshCookie(t *testing.T) {
	ctx, cleanup := withTx(t)
	defer cleanup()

	h := testAPIHandler(t)
	h.verifier = fakeSocialVerifier{
		profile: user.SocialProfile{
			Provider:       "github",
			ProviderUserID: "api-login-" + strconv.FormatInt(time.Now().UnixNano(), 10),
			Email:          "api-login+" + strconv.FormatInt(time.Now().UnixNano(), 10) + "@example.com",
			EmailVerified:  true,
			Name:           "API Login User",
			Username:       "apilogin",
		},
	}

	body := map[string]any{
		"code":          "code-1",
		"code_verifier": "verifier-1",
		"redirect_uri":  "http://127.0.0.1:8080/callback",
	}
	req := jsonRequest(t, http.MethodPost, "/api/auth/login/github", body)
	req = req.WithContext(ctx)
	req = withURLParam(req, "provider", "github")
	rec := httptest.NewRecorder()

	h.apiLogin(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", rec.Code, rec.Body.String())
	}

	var payload struct {
		TokenType    string              `json:"token_type"`
		AccessToken  string              `json:"access_token"`
		RefreshToken string              `json:"refresh_token"`
		User         apiAuthUserResponse `json:"user"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.TokenType != "bearer" || payload.AccessToken == "" || payload.RefreshToken == "" {
		t.Fatalf("expected token pair in response")
	}
	if payload.User.ID == 0 {
		t.Fatalf("expected user in response")
	}
	assertRefreshCookieSet(t, rec, h.apiRefreshCookieName)
}

func TestAPIRefreshRotatesAndDetectsReuse(t *testing.T) {
	ctx := context.Background()

	h := testAPIHandler(t)
	u, _, _ := insertUserAndSession(t, ctx, h.users)
	issued, err := h.issueAPITokenPair(ctx, u.ID, time.Now())
	if err != nil {
		t.Fatalf("issue api token pair: %v", err)
	}

	refreshReq := jsonRequest(t, http.MethodPost, "/api/auth/refresh", map[string]any{"refresh_token": issued.RefreshToken})
	refreshReq = refreshReq.WithContext(ctx)
	refreshRec := httptest.NewRecorder()
	h.apiRefresh(refreshRec, refreshReq)
	if refreshRec.Code != http.StatusOK {
		t.Fatalf("unexpected refresh status: %d body=%s", refreshRec.Code, refreshRec.Body.String())
	}
	var refreshPayload apiTokenResponse
	if err := json.Unmarshal(refreshRec.Body.Bytes(), &refreshPayload); err != nil {
		t.Fatalf("decode refresh response: %v", err)
	}
	if refreshPayload.AccessToken == "" || refreshPayload.RefreshToken == "" {
		t.Fatalf("expected rotated token pair")
	}
	if refreshPayload.RefreshToken == issued.RefreshToken {
		t.Fatalf("expected refresh token rotation")
	}

	reuseReq := jsonRequest(t, http.MethodPost, "/api/auth/refresh", map[string]any{"refresh_token": issued.RefreshToken})
	reuseReq = reuseReq.WithContext(ctx)
	reuseRec := httptest.NewRecorder()
	h.apiRefresh(reuseRec, reuseReq)
	if reuseRec.Code != http.StatusUnauthorized {
		t.Fatalf("expected unauthorized on reuse, got %d", reuseRec.Code)
	}

	// Family should be revoked after reuse detection, so the rotated token should also fail.
	revokedFamilyReq := jsonRequest(t, http.MethodPost, "/api/auth/refresh", map[string]any{"refresh_token": refreshPayload.RefreshToken})
	revokedFamilyReq = revokedFamilyReq.WithContext(ctx)
	revokedFamilyRec := httptest.NewRecorder()
	h.apiRefresh(revokedFamilyRec, revokedFamilyReq)
	if revokedFamilyRec.Code != http.StatusUnauthorized {
		t.Fatalf("expected unauthorized after family revocation, got %d", revokedFamilyRec.Code)
	}
}

func TestAPILogoutRevokesRefreshToken(t *testing.T) {
	ctx := context.Background()

	h := testAPIHandler(t)
	u, _, _ := insertUserAndSession(t, ctx, h.users)
	issued, err := h.issueAPITokenPair(ctx, u.ID, time.Now())
	if err != nil {
		t.Fatalf("issue api token pair: %v", err)
	}

	req := jsonRequest(t, http.MethodPost, "/api/auth/logout", map[string]any{"refresh_token": issued.RefreshToken})
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	h.apiLogout(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("unexpected status: %d", rec.Code)
	}
	assertCookieCleared(t, rec, h.apiRefreshCookieName)

	row, err := h.users.GetAPIRefreshTokenByHash(ctx, hashToken(issued.RefreshToken))
	if err != nil {
		t.Fatalf("load refresh token: %v", err)
	}
	if row.RevokedAt == nil {
		t.Fatalf("expected refresh token to be revoked")
	}
}

func TestAPIMeRequiresValidJWT(t *testing.T) {
	ctx, cleanup := withTx(t)
	defer cleanup()

	h := testAPIHandler(t)
	u, _, _ := insertUserAndSession(t, ctx, h.users)
	accessToken, _, err := h.issueAPIAccessToken(u.ID, "api-session-family-1", time.Now())
	if err != nil {
		t.Fatalf("issue access token: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	h.requireAPIAuth(http.HandlerFunc(h.apiMe)).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", rec.Code, rec.Body.String())
	}
	var payload apiAuthUserResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode me response: %v", err)
	}
	if payload.ID != u.ID {
		t.Fatalf("unexpected user id: got %d want %d", payload.ID, u.ID)
	}
}

func TestAPILoginHandlesVerifierFailure(t *testing.T) {
	ctx, cleanup := withTx(t)
	defer cleanup()

	h := testAPIHandler(t)
	h.verifier = fakeSocialVerifier{err: errors.New("oauth failed")}

	req := jsonRequest(t, http.MethodPost, "/api/auth/login/google", map[string]any{
		"code":          "code",
		"code_verifier": "verifier",
		"redirect_uri":  "http://127.0.0.1:8080/cb",
	})
	req = req.WithContext(ctx)
	req = withURLParam(req, "provider", "google")
	rec := httptest.NewRecorder()
	h.apiLogin(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected unauthorized, got %d", rec.Code)
	}
}

func testAPIHandler(t *testing.T) handler {
	t.Helper()
	cfg := config.Config{
		AppName: "Go Starter",
		AppEnv:  "test",
		AppURL:  "http://127.0.0.1:8080",
		Auth: config.AuthConfig{
			SessionCookieName: "test_session",
			SessionTTL:        30 * 24 * time.Hour,
			Social: config.SocialAuthConfig{
				Google: config.OAuthClientConfig{ClientID: "google-client", ClientSecret: "google-secret"},
				GitHub: config.OAuthClientConfig{ClientID: "github-client", ClientSecret: "github-secret"},
			},
			API: config.APIAuthConfig{
				AccessTokenSecret: "test-api-access-secret",
				AccessTokenTTL:    10 * time.Minute,
				RefreshTokenTTL:   24 * time.Hour,
				RefreshCookieName: "test_api_refresh",
			},
		},
	}
	return newHandler(integrationPool, cfg)
}

func jsonRequest(t *testing.T, method, path string, body any) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encode json request: %v", err)
		}
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	return req
}

func assertRefreshCookieSet(t *testing.T, rec *httptest.ResponseRecorder, cookieName string) {
	t.Helper()
	for _, c := range rec.Result().Cookies() {
		if c.Name == cookieName && c.Value != "" && c.HttpOnly {
			return
		}
	}
	t.Fatalf("expected refresh cookie %q to be set", cookieName)
}

func withURLParam(req *http.Request, key, value string) *http.Request {
	routeCtx := chi.NewRouteContext()
	routeCtx.URLParams.Add(key, value)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, routeCtx))
}
