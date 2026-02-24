package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/a-h/templ"
	"github.com/benpsk/go-starter/internal/config"
	"github.com/benpsk/go-starter/internal/postgres"
	"github.com/benpsk/go-starter/internal/user"
	"github.com/benpsk/go-starter/internal/web/components"
	"github.com/benpsk/go-starter/internal/web/pages"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type handler struct {
	db                       *pgxpool.Pool
	users                    *postgres.UserAuthStore
	appName                  string
	appEnv                   string
	appURL                   string
	googleTagID              string
	sessionCookieName        string
	sessionTTL               time.Duration
	sessionCookieForceSecure bool
	apiAccessTokenSecret     string
	apiAccessTokenTTL        time.Duration
	apiRefreshTokenTTL       time.Duration
	apiRefreshCookieName     string
	oauthFlows               *oauthFlowStore
	verifier                 socialAuthVerifier
	googleOAuth              oauthProviderConfig
	githubOAuth              oauthProviderConfig
}

func newHandler(db *pgxpool.Pool, cfg config.Config) handler {
	return handler{
		db:                       db,
		users:                    postgres.NewUserAuthStore(db),
		appName:                  cfg.AppName,
		appEnv:                   cfg.AppEnv,
		appURL:                   cfg.AppURL,
		googleTagID:              cfg.GoogleTagID,
		sessionCookieName:        cfg.Auth.SessionCookieName,
		sessionTTL:               cfg.Auth.SessionTTL,
		sessionCookieForceSecure: cfg.Auth.CookieSecure,
		apiAccessTokenSecret:     cfg.Auth.API.AccessTokenSecret,
		apiAccessTokenTTL:        cfg.Auth.API.AccessTokenTTL,
		apiRefreshTokenTTL:       cfg.Auth.API.RefreshTokenTTL,
		apiRefreshCookieName:     cfg.Auth.API.RefreshCookieName,
		oauthFlows:               newOAuthFlowStore(6 * time.Minute),
		verifier:                 newSocialVerifier(),
		googleOAuth: oauthProviderConfig{
			ClientID:     cfg.Auth.Social.Google.ClientID,
			ClientSecret: cfg.Auth.Social.Google.ClientSecret,
		},
		githubOAuth: oauthProviderConfig{
			ClientID:     cfg.Auth.Social.GitHub.ClientID,
			ClientSecret: cfg.Auth.Social.GitHub.ClientSecret,
		},
	}
}

func (h handler) homePage(w http.ResponseWriter, r *http.Request) {
	if isHtmx(r) {
		h.renderPage(w, r, components.Content(h.appName, pages.HomeContent()))
		return
	}
	h.renderPage(w, r, pages.HomePage(h.appName, h.appURL, h.googleTagID, h.headerAuthData(r)))
}

func (h handler) aboutPage(w http.ResponseWriter, r *http.Request) {
	if isHtmx(r) {
		h.renderPage(w, r, components.Content("About | "+h.appName, pages.AboutContent()))
		return
	}
	h.renderPage(w, r, pages.AboutPage(h.appName, h.appURL, h.googleTagID, h.headerAuthData(r)))
}

func (h handler) healthz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	payload := map[string]any{"status": "ok", "database": "up"}
	status := http.StatusOK

	if h.db != nil {
		if err := h.db.Ping(ctx); err != nil {
			payload["status"] = "degraded"
			payload["database"] = err.Error()
			status = http.StatusServiceUnavailable
		}
	}

	writeJSON(w, status, payload)
}

func (h handler) notFoundPage(w http.ResponseWriter, r *http.Request) {
	if isHtmx(r) {
		h.renderPageStatus(w, r, http.StatusNotFound, components.Content("Not Found | "+h.appName, pages.NotFoundContent()))
		return
	}
	h.renderPageStatus(w, r, http.StatusNotFound, pages.NotFoundPage(h.appName, h.appURL, h.googleTagID, h.headerAuthData(r)))
}

func (h handler) methodNotAllowedPage(w http.ResponseWriter, r *http.Request) {
	if isHtmx(r) {
		h.renderPageStatus(w, r, http.StatusMethodNotAllowed, components.Content("Method Not Allowed | "+h.appName, pages.MethodNotAllowedContent()))
		return
	}
	h.renderPageStatus(w, r, http.StatusMethodNotAllowed, pages.MethodNotAllowedPage(h.appName, h.appURL, h.googleTagID, h.headerAuthData(r)))
}

func isHtmx(r *http.Request) bool {
	return r.Header.Get("HX-Request") == "true"
}

func (h handler) renderPage(w http.ResponseWriter, r *http.Request, component templ.Component) {
	h.renderPageStatus(w, r, http.StatusOK, component)
}

func (h handler) renderPageStatus(w http.ResponseWriter, r *http.Request, status int, component templ.Component) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := component.Render(r.Context(), w); err != nil {
		http.Error(w, "failed to render page", http.StatusInternalServerError)
	}
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if payload == nil {
		return
	}
	_ = json.NewEncoder(w).Encode(payload)
}

func (h handler) headerAuthData(r *http.Request) components.HeaderAuthData {
	currentUser := currentUserFromContext(r)
	if currentUser == nil {
		return components.HeaderAuthData{}
	}
	name := currentUser.DisplayName
	if name == "" {
		name = "Account"
	}
	return components.HeaderAuthData{
		IsAuthenticated: true,
		DisplayName:     name,
		AvatarURL:       currentUser.AvatarURL,
	}
}

func providerEnabled(cfg oauthProviderConfig) bool {
	return cfg.ClientID != "" && cfg.ClientSecret != ""
}

func (h handler) loginPage(w http.ResponseWriter, r *http.Request) {
	errMessage := ""
	switch strings.TrimSpace(r.URL.Query().Get("error")) {
	case "provider_not_configured":
		errMessage = "Provider is not configured yet."
	case "oauth_failed":
		errMessage = "Sign in failed. Please try again."
	case "account_conflict":
		errMessage = "An account with the same email already exists under another provider. Linking is not supported in this starter yet."
	}
	model := pages.LoginPageModel{
		AppName:       h.appName,
		AppURL:        h.appURL,
		GoogleTagID:   h.googleTagID,
		Auth:          h.headerAuthData(r),
		Error:         errMessage,
		GoogleEnabled: providerEnabled(h.googleOAuth),
		GitHubEnabled: providerEnabled(h.githubOAuth),
	}
	h.renderPage(w, r, pages.LoginPage(model))
}

func (h handler) accountPage(w http.ResponseWriter, r *http.Request) {
	currentUser := currentUserFromContext(r)
	if currentUser == nil {
		http.Redirect(w, r, "/auth/login", http.StatusSeeOther)
		return
	}
	identities, err := h.users.ListIdentitiesByUserID(r.Context(), currentUser.ID)
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

func (h handler) startSocialLogin(w http.ResponseWriter, r *http.Request) {
	provider := strings.TrimSpace(strings.ToLower(chi.URLParam(r, "provider")))
	if provider == "" {
		http.Redirect(w, r, "/auth/login?error=oauth_failed", http.StatusSeeOther)
		return
	}
	cfg, ok := h.oauthProviderConfig(provider)
	if !ok || !providerEnabled(cfg) {
		http.Redirect(w, r, "/auth/login?error=provider_not_configured", http.StatusSeeOther)
		return
	}
	redirectTo := strings.TrimSpace(r.FormValue("next"))
	if redirectTo == "" || !strings.HasPrefix(redirectTo, "/") || strings.HasPrefix(redirectTo, "//") {
		redirectTo = "/account"
	}
	record, err := h.oauthFlows.create(provider, redirectTo, time.Now())
	if err != nil {
		http.Redirect(w, r, "/auth/login?error=oauth_failed", http.StatusSeeOther)
		return
	}
	authURL := h.oauthAuthorizationURL(provider, cfg, record)
	http.Redirect(w, r, authURL, http.StatusSeeOther)
}

func (h handler) oauthCallback(w http.ResponseWriter, r *http.Request) {
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
	flow, err := h.oauthFlows.consume(state, provider, time.Now())
	if err != nil {
		http.Redirect(w, r, "/auth/login?error=oauth_failed", http.StatusSeeOther)
		return
	}
	cfg, ok := h.oauthProviderConfig(provider)
	if !ok {
		http.Redirect(w, r, "/auth/login?error=provider_not_configured", http.StatusSeeOther)
		return
	}
	profile, err := h.verifier.ExchangeAndVerify(r.Context(), provider, code, flow.CodeVerifier, h.oauthCallbackURL(provider), cfg)
	if err != nil {
		http.Redirect(w, r, "/auth/login?error=oauth_failed", http.StatusSeeOther)
		return
	}
	currentUser, err := h.findOrCreateSocialUser(r.Context(), profile)
	if err != nil {
		if errors.Is(err, user.ErrEmailConflict) {
			http.Redirect(w, r, "/auth/login?error=account_conflict", http.StatusSeeOther)
			return
		}
		http.Redirect(w, r, "/auth/login?error=oauth_failed", http.StatusSeeOther)
		return
	}
	token, expiresAt, err := h.createSession(r.Context(), currentUser, requestMetaFromRequest(r))
	if err != nil {
		http.Redirect(w, r, "/auth/login?error=oauth_failed", http.StatusSeeOther)
		return
	}
	h.setSessionCookie(w, r, token, expiresAt)
	http.Redirect(w, r, flow.RedirectTo, http.StatusSeeOther)
}

func (h handler) logout(w http.ResponseWriter, r *http.Request) {
	token := h.sessionTokenFromRequest(r)
	if token != "" {
		_ = h.users.DeleteSessionByTokenHash(r.Context(), hashToken(token))
	}
	h.clearSessionCookie(w, r)
	http.Redirect(w, r, "/auth/login", http.StatusSeeOther)
}
