package web

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/benpsk/go-starter/internal/auth"
	"github.com/benpsk/go-starter/internal/user"
	"github.com/benpsk/go-starter/internal/web/pages"
	"github.com/go-chi/chi/v5"
)

func (h Handler) loginPage(w http.ResponseWriter, r *http.Request) {
	errMessage := ""
	switch strings.TrimSpace(r.URL.Query().Get("error")) {
	case "provider_not_configured":
		errMessage = "Provider is not configured yet."
	case "oauth_failed":
		errMessage = "Sign in failed. Please try again."
	case "account_conflict":
		errMessage = "An account with the same email already exists under another provider. Linking is not supported in this starter yet."
	}
	googleCfg, _ := h.auth.ProviderConfig("google")
	githubCfg, _ := h.auth.ProviderConfig("github")
	model := pages.LoginPageModel{
		AppName:       h.appName,
		AppURL:        h.appURL,
		GoogleTagID:   h.googleTagID,
		Auth:          h.headerAuthData(r),
		Error:         errMessage,
		GoogleEnabled: auth.ProviderEnabled(googleCfg),
		GitHubEnabled: auth.ProviderEnabled(githubCfg),
	}
	h.renderPage(w, r, pages.LoginPage(model))
}

func (h Handler) accountPage(w http.ResponseWriter, r *http.Request) {
	currentUser := auth.CurrentUserFromRequest(r)
	if currentUser == nil {
		http.Redirect(w, r, "/auth/login", http.StatusSeeOther)
		return
	}
	identities, err := h.auth.Users().ListIdentitiesByUserID(r.Context(), currentUser.ID)
	if err != nil {
		http.Error(w, "failed to load account", http.StatusInternalServerError)
		return
	}
	model := pages.AccountPageModel{
		AppName:     h.appName,
		AppURL:      h.appURL,
		GoogleTagID: h.googleTagID,
		Auth:        h.headerAuthData(r),
		User:        *currentUser,
		Identities:  identities,
	}
	h.renderPage(w, r, pages.AccountPage(model))
}

func (h Handler) startSocialLogin(w http.ResponseWriter, r *http.Request) {
	provider := strings.TrimSpace(strings.ToLower(chi.URLParam(r, "provider")))
	if provider == "" {
		http.Redirect(w, r, "/auth/login?error=oauth_failed", http.StatusSeeOther)
		return
	}
	cfg, ok := h.auth.ProviderConfig(provider)
	if !ok || !auth.ProviderEnabled(cfg) {
		http.Redirect(w, r, "/auth/login?error=provider_not_configured", http.StatusSeeOther)
		return
	}
	redirectTo := strings.TrimSpace(r.FormValue("next"))
	if redirectTo == "" || !strings.HasPrefix(redirectTo, "/") || strings.HasPrefix(redirectTo, "//") {
		redirectTo = "/account"
	}
	record, err := h.auth.CreateOAuthFlow(provider, redirectTo, time.Now())
	if err != nil {
		http.Redirect(w, r, "/auth/login?error=oauth_failed", http.StatusSeeOther)
		return
	}
	authURL := h.auth.OAuthAuthorizationURL(provider, cfg, record)
	http.Redirect(w, r, authURL, http.StatusSeeOther)
}

func (h Handler) oauthCallback(w http.ResponseWriter, r *http.Request) {
	provider := strings.TrimSpace(strings.ToLower(chi.URLParam(r, "provider")))
	if provider == "" {
		http.Redirect(w, r, "/auth/login?error=oauth_failed", http.StatusSeeOther)
		return
	}
	if errParam := strings.TrimSpace(r.URL.Query().Get("error")); errParam != "" {
		http.Redirect(w, r, "/auth/login?error=oauth_failed", http.StatusSeeOther)
		return
	}
	code := strings.TrimSpace(r.URL.Query().Get("code"))
	state := strings.TrimSpace(r.URL.Query().Get("state"))
	if code == "" || state == "" {
		http.Redirect(w, r, "/auth/login?error=oauth_failed", http.StatusSeeOther)
		return
	}
	flow, err := h.auth.ConsumeOAuthFlow(state, provider, time.Now())
	if err != nil {
		http.Redirect(w, r, "/auth/login?error=oauth_failed", http.StatusSeeOther)
		return
	}
	cfg, ok := h.auth.ProviderConfig(provider)
	if !ok {
		http.Redirect(w, r, "/auth/login?error=provider_not_configured", http.StatusSeeOther)
		return
	}
	profile, err := h.auth.ExchangeAndVerify(r.Context(), provider, code, flow.CodeVerifier, h.auth.OAuthCallbackURL(provider), cfg)
	if err != nil {
		http.Redirect(w, r, "/auth/login?error=oauth_failed", http.StatusSeeOther)
		return
	}
	currentUser, err := h.auth.FindOrCreateSocialUser(r.Context(), profile)
	if err != nil {
		if errors.Is(err, user.ErrEmailConflict) {
			http.Redirect(w, r, "/auth/login?error=account_conflict", http.StatusSeeOther)
			return
		}
		http.Redirect(w, r, "/auth/login?error=oauth_failed", http.StatusSeeOther)
		return
	}
	token, expiresAt, err := h.auth.CreateSession(r.Context(), currentUser, auth.RequestMetaFromRequest(r))
	if err != nil {
		http.Redirect(w, r, "/auth/login?error=oauth_failed", http.StatusSeeOther)
		return
	}
	h.auth.SetSessionCookie(w, r, token, expiresAt)
	http.Redirect(w, r, flow.RedirectTo, http.StatusSeeOther)
}

func (h Handler) logout(w http.ResponseWriter, r *http.Request) {
	token := h.auth.SessionTokenFromRequest(r)
	if token != "" {
		_ = h.auth.Users().DeleteSessionByTokenHash(r.Context(), auth.HashToken(token))
	}
	h.auth.ClearSessionCookie(w, r)
	http.Redirect(w, r, "/auth/login", http.StatusSeeOther)
}
