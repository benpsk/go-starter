package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
	"time"

	"github.com/benpsk/go-starter/internal/user"
)

type authContextKey string

const currentUserContextKey authContextKey = "current_user"

func (h handler) loadSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if skipSessionLoad(r) {
			next.ServeHTTP(w, r)
			return
		}

		token := h.sessionTokenFromRequest(r)
		if token == "" {
			next.ServeHTTP(w, r)
			return
		}

		sess, currentUser, err := h.users.FindSessionAndUserByTokenHash(r.Context(), hashToken(token))
		if err != nil {
			h.clearSessionCookie(w, r)
			next.ServeHTTP(w, r)
			return
		}
		now := time.Now()
		if sess.RevokedAt != nil || now.After(sess.ExpiresAt) {
			h.clearSessionCookie(w, r)
			next.ServeHTTP(w, r)
			return
		}

		if now.Sub(sess.LastSeenAt) >= 10*time.Minute {
			_ = h.users.TouchSession(r.Context(), sess.ID, now)
		}

		ctx := context.WithValue(r.Context(), currentUserContextKey, &currentUser)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func skipSessionLoad(r *http.Request) bool {
	if r == nil || r.URL == nil {
		return false
	}
	path := strings.TrimSpace(r.URL.Path)
	if path == "" {
		return false
	}
	if path == "/healthz" || path == "/api/health" {
		return true
	}
	return strings.HasPrefix(path, "/static/") || strings.HasPrefix(path, "/api/")
}

func (h handler) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if currentUserFromContext(r) == nil {
			if isHtmx(r) {
				w.Header().Set("HX-Redirect", "/auth/login")
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			http.Redirect(w, r, "/auth/login", http.StatusSeeOther)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (h handler) requireGuest(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if currentUserFromContext(r) != nil {
			http.Redirect(w, r, "/account", http.StatusSeeOther)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func currentUserFromContext(r *http.Request) *user.User {
	if r == nil {
		return nil
	}
	if u, ok := r.Context().Value(currentUserContextKey).(*user.User); ok {
		return u
	}
	return nil
}

func (h handler) createSession(ctx context.Context, currentUser user.User, meta requestMeta) (string, time.Time, error) {
	rawToken, err := randomToken(32)
	if err != nil {
		return "", time.Time{}, err
	}
	expiresAt := time.Now().Add(h.sessionTTL)
	err = h.users.CreateSession(ctx, user.Session{
		UserID:     currentUser.ID,
		TokenHash:  hashToken(rawToken),
		ExpiresAt:  expiresAt,
		LastSeenAt: time.Now(),
		IP:         meta.IP,
		UserAgent:  meta.UserAgent,
	})
	if err != nil {
		return "", time.Time{}, err
	}
	return rawToken, expiresAt, nil
}

func hashToken(raw string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(raw)))
	return hex.EncodeToString(sum[:])
}

type requestMeta struct {
	IP        string
	UserAgent string
}

func requestMetaFromRequest(r *http.Request) requestMeta {
	ip := normalizedClientIP(r)
	return requestMeta{
		IP:        ip,
		UserAgent: strings.TrimSpace(r.UserAgent()),
	}
}

func (h handler) setSessionCookie(w http.ResponseWriter, r *http.Request, token string, expiresAt time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     h.sessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   h.sessionCookieSecure(r),
		Expires:  expiresAt,
		MaxAge:   int(time.Until(expiresAt).Seconds()),
	})
}

func (h handler) clearSessionCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     h.sessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   h.sessionCookieSecure(r),
		MaxAge:   -1,
	})
}

func (h handler) sessionTokenFromRequest(r *http.Request) string {
	if c, err := r.Cookie(h.sessionCookieName); err == nil {
		return strings.TrimSpace(c.Value)
	}
	return ""
}

func (h handler) sessionCookieSecure(r *http.Request) bool {
	if h.sessionCookieForceSecure {
		return true
	}
	if strings.EqualFold(h.appEnv, "production") {
		return true
	}
	if r != nil && r.TLS != nil {
		return true
	}
	return r != nil && strings.EqualFold(strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")), "https")
}
