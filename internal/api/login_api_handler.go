package api

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/benpsk/go-starter/internal/apperror"
	"github.com/benpsk/go-starter/internal/authctx"
	loginsvc "github.com/benpsk/go-starter/internal/login"
	usersvc "github.com/benpsk/go-starter/internal/users"
	"github.com/go-chi/chi/v5"
)

// refreshCookiePath scopes the refresh cookie to the auth endpoints that use it.
const refreshCookiePath = "/api/auth"

// LoginAPIConfig carries the cookie settings for the refresh credential.
type LoginAPIConfig struct {
	RefreshCookieName string
	CookieSecure      bool
}

// LoginAPIHandler exposes the JSON authentication endpoints.
type LoginAPIHandler struct {
	svc   loginsvc.Service
	users usersvc.Service
	cfg   LoginAPIConfig
}

// NewLoginAPIHandler wires the login and users services into an HTTP handler.
func NewLoginAPIHandler(svc loginsvc.Service, userService usersvc.Service, cfg LoginAPIConfig) LoginAPIHandler {
	return LoginAPIHandler{svc: svc, users: userService, cfg: cfg}
}

type loginRequest struct {
	Code         string `json:"code"`
	CodeVerifier string `json:"code_verifier"`
	RedirectURI  string `json:"redirect_uri"`
}

type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

type userResponse struct {
	ID          int64  `json:"id"`
	Email       string `json:"email,omitempty"`
	DisplayName string `json:"display_name"`
	AvatarURL   string `json:"avatar_url,omitempty"`
}

type loginResponse struct {
	loginsvc.TokenPair
	User userResponse `json:"user"`
}

// Login completes a client-driven PKCE exchange and returns a token pair.
func (h LoginAPIHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := decodeJSON(w, r, &req); err != nil {
		if errors.Is(err, io.EOF) {
			Error(w, apperror.BadRequest("request body is required"))
			return
		}
		Error(w, err)
		return
	}

	provider := chi.URLParam(r, "provider")
	currentUser, err := h.svc.ExchangeCode(r.Context(), provider, req.Code, req.CodeVerifier, req.RedirectURI)
	if err != nil {
		Error(w, err)
		return
	}

	tokens, err := h.svc.IssueTokenPair(r.Context(), currentUser.ID, time.Now())
	if err != nil {
		Error(w, err)
		return
	}
	h.setRefreshCookie(w, tokens.RefreshToken, tokens.RefreshTokenExpiresAt)
	Success(w, http.StatusOK, loginResponse{TokenPair: tokens, User: toUserResponse(currentUser)})
}

// Refresh rotates the refresh token and returns a fresh pair.
func (h LoginAPIHandler) Refresh(w http.ResponseWriter, r *http.Request) {
	refreshToken, err := h.refreshTokenFromRequest(w, r)
	if err != nil {
		Error(w, err)
		return
	}
	if refreshToken == "" {
		Error(w, apperror.BadRequest("refresh_token is required"))
		return
	}

	tokens, err := h.svc.RotateRefreshToken(r.Context(), refreshToken, time.Now())
	if err != nil {
		h.clearRefreshCookie(w)
		Error(w, err)
		return
	}
	h.setRefreshCookie(w, tokens.RefreshToken, tokens.RefreshTokenExpiresAt)
	Success(w, http.StatusOK, tokens)
}

// Logout revokes the presented refresh token. It is idempotent.
func (h LoginAPIHandler) Logout(w http.ResponseWriter, r *http.Request) {
	refreshToken, err := h.refreshTokenFromRequest(w, r)
	if err != nil {
		Error(w, err)
		return
	}
	if refreshToken != "" {
		if err := h.svc.RevokeRefreshToken(r.Context(), refreshToken, time.Now()); err != nil {
			Error(w, err)
			return
		}
	}
	h.clearRefreshCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

// Me returns the authenticated user record.
func (h LoginAPIHandler) Me(w http.ResponseWriter, r *http.Request) {
	principal, ok := authctx.UserFromContext(r.Context())
	if !ok {
		Error(w, apperror.Unauthorized("unauthorized"))
		return
	}
	currentUser, err := h.users.FindByID(r.Context(), principal.ID)
	if err != nil {
		Error(w, err)
		return
	}
	Success(w, http.StatusOK, toUserResponse(currentUser))
}

// refreshTokenFromRequest prefers the HttpOnly cookie and falls back to the
// JSON body, so both browser and native clients work unchanged.
func (h LoginAPIHandler) refreshTokenFromRequest(w http.ResponseWriter, r *http.Request) (string, error) {
	if cookie, err := r.Cookie(h.cfg.RefreshCookieName); err == nil {
		if value := strings.TrimSpace(cookie.Value); value != "" {
			return value, nil
		}
	}

	var req refreshRequest
	if err := decodeJSON(w, r, &req); err != nil {
		if errors.Is(err, io.EOF) {
			return "", nil
		}
		return "", err
	}
	return strings.TrimSpace(req.RefreshToken), nil
}

func (h LoginAPIHandler) setRefreshCookie(w http.ResponseWriter, token string, expiresAt time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     h.cfg.RefreshCookieName,
		Value:    token,
		Path:     refreshCookiePath,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   h.cfg.CookieSecure,
		Expires:  expiresAt,
		MaxAge:   int(time.Until(expiresAt).Seconds()),
	})
}

func (h LoginAPIHandler) clearRefreshCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     h.cfg.RefreshCookieName,
		Value:    "",
		Path:     refreshCookiePath,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   h.cfg.CookieSecure,
		MaxAge:   -1,
	})
}

func toUserResponse(in usersvc.User) userResponse {
	return userResponse{
		ID:          in.ID,
		Email:       in.Email,
		DisplayName: in.DisplayName,
		AvatarURL:   in.AvatarURL,
	}
}
