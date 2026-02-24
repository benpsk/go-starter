package server

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/benpsk/go-starter/internal/user"
)

func (h handler) issueAPITokenPair(ctx context.Context, userID int64, now time.Time) (apiTokenResponse, error) {
	familyID, err := randomToken(20)
	if err != nil {
		return apiTokenResponse{}, err
	}
	refreshToken, err := randomToken(32)
	if err != nil {
		return apiTokenResponse{}, err
	}
	refreshExpiresAt := now.Add(h.apiRefreshTokenTTL)
	if err := h.users.CreateAPIRefreshToken(ctx, user.APIRefreshToken{
		UserID:    userID,
		FamilyID:  familyID,
		TokenHash: hashToken(refreshToken),
		ExpiresAt: refreshExpiresAt,
	}); err != nil {
		return apiTokenResponse{}, err
	}

	accessToken, accessExpiresAt, err := h.issueAPIAccessToken(userID, familyID, now)
	if err != nil {
		return apiTokenResponse{}, err
	}

	return apiTokenResponse{
		TokenType:             "bearer",
		AccessToken:           accessToken,
		AccessTokenExpiresAt:  accessExpiresAt,
		RefreshToken:          refreshToken,
		RefreshTokenExpiresAt: refreshExpiresAt,
	}, nil
}

func (h handler) rotateAPIRefreshToken(ctx context.Context, currentRefreshToken string, now time.Time) (apiTokenResponse, error) {
	currentHash := hashToken(currentRefreshToken)
	newRefreshToken, err := randomToken(32)
	if err != nil {
		return apiTokenResponse{}, err
	}
	result, err := h.users.RotateAPIRefreshToken(ctx, currentHash, user.APIRefreshToken{
		TokenHash: hashToken(newRefreshToken),
		ExpiresAt: now.Add(h.apiRefreshTokenTTL),
	}, now)
	if err != nil {
		return apiTokenResponse{}, err
	}
	if !result.Authorized {
		if result.ReuseDetected && result.FamilyID != "" {
			_ = h.users.RevokeAPIRefreshTokenFamily(ctx, result.FamilyID, now)
		}
		return apiTokenResponse{}, errors.New("unauthorized")
	}

	// Store method preserves family and user, but needs new row user/family from inputs; issue a second pass if family not set.
	// Current store implementation returns resolved user/family.
	accessToken, accessExpiresAt, err := h.issueAPIAccessToken(result.UserID, result.FamilyID, now)
	if err != nil {
		return apiTokenResponse{}, err
	}
	return apiTokenResponse{
		TokenType:             "bearer",
		AccessToken:           accessToken,
		AccessTokenExpiresAt:  accessExpiresAt,
		RefreshToken:          newRefreshToken,
		RefreshTokenExpiresAt: now.Add(h.apiRefreshTokenTTL),
	}, nil
}

func (h handler) apiRefreshTokenFromRequest(r *http.Request) string {
	if r == nil {
		return ""
	}
	if c, err := r.Cookie(h.apiRefreshCookieName); err == nil {
		return strings.TrimSpace(c.Value)
	}
	return ""
}

func (h handler) setAPIRefreshCookie(w http.ResponseWriter, r *http.Request, token string, expiresAt time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     h.apiRefreshCookieName,
		Value:    token,
		Path:     "/api/auth",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   h.sessionCookieSecure(r),
		Expires:  expiresAt,
		MaxAge:   int(time.Until(expiresAt).Seconds()),
	})
}

func (h handler) clearAPIRefreshCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     h.apiRefreshCookieName,
		Value:    "",
		Path:     "/api/auth",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   h.sessionCookieSecure(r),
		MaxAge:   -1,
	})
}
