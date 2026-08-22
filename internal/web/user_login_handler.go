package web

import (
	"net/http"
	"strings"
	"time"

	usersession "github.com/benpsk/go-starter/internal/auth/user"
	"github.com/benpsk/go-starter/internal/config"
	loginsvc "github.com/benpsk/go-starter/internal/login"
	"github.com/benpsk/go-starter/internal/web/components"
	"github.com/benpsk/go-starter/internal/web/pages"
	"github.com/go-chi/chi/v5"
)

// Login page paths and the query values used to surface a failure reason.
const (
	loginPath              = "/auth/login"
	defaultPostLoginPath   = "/account"
	errProviderUnavailable = "provider_not_configured"
	errOAuthFailed         = "oauth_failed"
	errAccountConflict     = "account_conflict"
)

// UserLoginHandler drives the browser social sign-in flow.
type UserLoginHandler struct {
	svc      loginsvc.Service
	sessions *usersession.Manager
	setting  config.Setting
}

// NewUserLoginHandler wires the login service and session manager together.
func NewUserLoginHandler(svc loginsvc.Service, sessions *usersession.Manager, setting config.Setting) UserLoginHandler {
	return UserLoginHandler{svc: svc, sessions: sessions, setting: setting}
}

// Show renders the sign-in page, including any failure from a prior attempt.
func (h UserLoginHandler) Show(w http.ResponseWriter, r *http.Request) {
	props := components.LoginProps{
		PageProps: pageProps(h.setting, r, components.Meta{
			Title:       "Login",
			Description: "Sign in with Google or GitHub.",
			Keywords:    "login,oauth,google,github",
			Path:        loginPath,
		}),
		Error:         loginErrorMessage(r.URL.Query().Get("error")),
		GoogleEnabled: h.svc.ProviderEnabled(loginsvc.ProviderGoogle),
		GitHubEnabled: h.svc.ProviderEnabled(loginsvc.ProviderGitHub),
	}
	render(w, r, pages.LoginPage(props))
}

// Start redirects the browser to the provider's authorization screen.
func (h UserLoginHandler) Start(w http.ResponseWriter, r *http.Request) {
	provider := chi.URLParam(r, "provider")
	start, err := h.svc.StartOAuth(provider, safeRedirectPath(r.FormValue("next")), time.Now())
	if err != nil {
		h.redirectWithError(w, r, err)
		return
	}
	http.Redirect(w, r, start.AuthorizationURL, http.StatusSeeOther)
}

// Callback completes the provider round trip and opens a browser session.
func (h UserLoginHandler) Callback(w http.ResponseWriter, r *http.Request) {
	provider := chi.URLParam(r, "provider")
	query := r.URL.Query()
	if strings.TrimSpace(query.Get("error")) != "" {
		http.Redirect(w, r, loginPath+"?error="+errOAuthFailed, http.StatusSeeOther)
		return
	}

	completed, err := h.svc.CompleteOAuth(r.Context(), provider, query.Get("state"), query.Get("code"), time.Now())
	if err != nil {
		h.redirectWithError(w, r, err)
		return
	}
	if err := h.sessions.Issue(r.Context(), w, r, completed.User.ID); err != nil {
		http.Redirect(w, r, loginPath+"?error="+errOAuthFailed, http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, safeRedirectPath(completed.RedirectTo), http.StatusSeeOther)
}

// Logout clears the browser session.
func (h UserLoginHandler) Logout(w http.ResponseWriter, r *http.Request) {
	h.sessions.Clear(r.Context(), w, r)
	http.Redirect(w, r, loginPath, http.StatusSeeOther)
}

// redirectWithError maps a service failure onto the sign-in page's error query,
// keeping provider-level detail out of the browser.
func (h UserLoginHandler) redirectWithError(w http.ResponseWriter, r *http.Request, err error) {
	reason := errOAuthFailed
	switch {
	case loginsvc.IsProviderNotConfigured(err):
		reason = errProviderUnavailable
	case isConflict(err):
		reason = errAccountConflict
	}
	http.Redirect(w, r, loginPath+"?error="+reason, http.StatusSeeOther)
}

func loginErrorMessage(code string) string {
	switch strings.TrimSpace(code) {
	case errProviderUnavailable:
		return "Provider is not configured yet."
	case errOAuthFailed:
		return "Sign in failed. Please try again."
	case errAccountConflict:
		return "An account with the same email already exists under another provider. Linking is not supported in this starter yet."
	default:
		return ""
	}
}

// safeRedirectPath rejects absolute and protocol-relative targets so a
// redirect cannot be pointed off-site.
func safeRedirectPath(target string) string {
	trimmed := strings.TrimSpace(target)
	if trimmed == "" || !strings.HasPrefix(trimmed, "/") || strings.HasPrefix(trimmed, "//") {
		return defaultPostLoginPath
	}
	return trimmed
}
