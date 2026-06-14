package httpapi

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/benpsk/go-starter/internal/auth"
	"github.com/benpsk/go-starter/internal/user"
	"github.com/go-chi/chi/v5"
)

type apiAuthContextKey string

const apiAuthClaimsKey apiAuthContextKey = "api_auth_claims"

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

func (h Handler) login(w http.ResponseWriter, r *http.Request) {
	if !h.auth.APIAuthConfigured() {
		writeErrorJSON(w, http.StatusServiceUnavailable, "api auth is not configured")
		return
	}
	provider := strings.TrimSpace(strings.ToLower(chi.URLParam(r, "provider")))
	cfg, ok := h.auth.ProviderConfig(provider)
	if !ok || !auth.ProviderEnabled(cfg) {
		writeErrorJSON(w, http.StatusBadRequest, "provider is not configured")
		return
	}

	var req loginRequest
	if err := decodeJSONWithLimit(w, r, &req, defaultRequestBodyLimitBytes); err != nil {
		if isRequestBodyTooLarge(err) {
			writeErrorJSON(w, http.StatusRequestEntityTooLarge, "request body too large")
			return
		}
		writeErrorJSON(w, http.StatusBadRequest, "invalid json")
		return
	}
	if strings.TrimSpace(req.Code) == "" || strings.TrimSpace(req.CodeVerifier) == "" || strings.TrimSpace(req.RedirectURI) == "" {
		writeErrorJSON(w, http.StatusBadRequest, "code, code_verifier, and redirect_uri are required")
		return
	}

	profile, err := h.auth.ExchangeAndVerify(r.Context(), provider, req.Code, req.CodeVerifier, strings.TrimSpace(req.RedirectURI), cfg)
	if err != nil {
		writeErrorJSON(w, http.StatusUnauthorized, "oauth login failed")
		return
	}
	currentUser, err := h.auth.FindOrCreateSocialUser(r.Context(), profile)
	if err != nil {
		if errors.Is(err, user.ErrEmailConflict) {
			writeErrorJSON(w, http.StatusConflict, "account email is already used by another provider")
			return
		}
		writeErrorJSON(w, http.StatusInternalServerError, "failed to sign in user")
		return
	}

	resp, err := h.auth.IssueAPITokenPair(r.Context(), currentUser.ID, time.Now())
	if err != nil {
		writeErrorJSON(w, http.StatusInternalServerError, "failed to issue tokens")
		return
	}
	h.auth.SetAPIRefreshCookie(w, r, resp.RefreshToken, resp.RefreshTokenExpiresAt)
	writeJSON(w, http.StatusOK, map[string]any{
		"token_type":               resp.TokenType,
		"access_token":             resp.AccessToken,
		"access_token_expires_at":  resp.AccessTokenExpiresAt,
		"refresh_token":            resp.RefreshToken,
		"refresh_token_expires_at": resp.RefreshTokenExpiresAt,
		"user": userResponse{
			ID:          currentUser.ID,
			Email:       currentUser.Email,
			DisplayName: currentUser.DisplayName,
			AvatarURL:   currentUser.AvatarURL,
		},
	})
}

func (h Handler) refresh(w http.ResponseWriter, r *http.Request) {
	if !h.auth.APIAuthConfigured() {
		writeErrorJSON(w, http.StatusServiceUnavailable, "api auth is not configured")
		return
	}
	refreshToken := h.auth.APIRefreshTokenFromRequest(r)
	if refreshToken == "" {
		var req refreshRequest
		if err := decodeJSONWithLimit(w, r, &req, defaultRequestBodyLimitBytes); err == nil {
			refreshToken = strings.TrimSpace(req.RefreshToken)
		} else if isRequestBodyTooLarge(err) {
			writeErrorJSON(w, http.StatusRequestEntityTooLarge, "request body too large")
			return
		} else if !errors.Is(err, io.EOF) {
			writeErrorJSON(w, http.StatusBadRequest, "invalid json")
			return
		}
	}
	if refreshToken == "" {
		writeErrorJSON(w, http.StatusBadRequest, "refresh_token is required")
		return
	}
	resp, err := h.auth.RotateAPIRefreshToken(r.Context(), refreshToken, time.Now())
	if err != nil {
		writeErrorJSON(w, http.StatusUnauthorized, "invalid refresh token")
		return
	}
	h.auth.SetAPIRefreshCookie(w, r, resp.RefreshToken, resp.RefreshTokenExpiresAt)
	writeJSON(w, http.StatusOK, resp)
}

func (h Handler) logout(w http.ResponseWriter, r *http.Request) {
	refreshToken := h.auth.APIRefreshTokenFromRequest(r)
	if refreshToken == "" {
		var req refreshRequest
		if err := decodeJSONWithLimit(w, r, &req, defaultRequestBodyLimitBytes); err == nil {
			refreshToken = strings.TrimSpace(req.RefreshToken)
		} else if isRequestBodyTooLarge(err) {
			writeErrorJSON(w, http.StatusRequestEntityTooLarge, "request body too large")
			return
		} else if !errors.Is(err, io.EOF) {
			writeErrorJSON(w, http.StatusBadRequest, "invalid json")
			return
		}
	}
	if refreshToken != "" {
		_ = h.auth.Users().RevokeAPIRefreshTokenByHash(r.Context(), auth.HashToken(refreshToken), time.Now())
	}
	h.auth.ClearAPIRefreshCookie(w, r)
	w.WriteHeader(http.StatusNoContent)
}

func (h Handler) me(w http.ResponseWriter, r *http.Request) {
	claims := apiAuthFromContext(r)
	if claims == nil {
		writeErrorJSON(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	currentUser, err := h.auth.Users().FindByID(r.Context(), claims.UserID)
	if err != nil {
		writeErrorJSON(w, http.StatusUnauthorized, "user not found")
		return
	}
	writeJSON(w, http.StatusOK, userResponse{
		ID:          currentUser.ID,
		Email:       currentUser.Email,
		DisplayName: currentUser.DisplayName,
		AvatarURL:   currentUser.AvatarURL,
	})
}

func (h Handler) requireAPIAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := auth.BearerTokenFromRequest(r)
		claims, err := h.auth.ParseAPIAccessToken(token)
		if err != nil {
			writeErrorJSON(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		ctx := context.WithValue(r.Context(), apiAuthClaimsKey, &claims)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func apiAuthFromContext(r *http.Request) *auth.ParsedAPIAccessToken {
	if r == nil {
		return nil
	}
	if claims, ok := r.Context().Value(apiAuthClaimsKey).(*auth.ParsedAPIAccessToken); ok {
		return claims
	}
	return nil
}
