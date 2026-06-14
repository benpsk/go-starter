package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
	"time"

	"github.com/benpsk/go-starter/internal/user"
)

type contextKey string

const currentUserContextKey contextKey = "current_user"

func (s *Service) LoadSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if skipSessionLoad(r) {
			next.ServeHTTP(w, r)
			return
		}

		token := s.SessionTokenFromRequest(r)
		if token == "" {
			next.ServeHTTP(w, r)
			return
		}

		sess, currentUser, err := s.users.FindSessionAndUserByTokenHash(r.Context(), HashToken(token))
		if err != nil {
			s.ClearSessionCookie(w, r)
			next.ServeHTTP(w, r)
			return
		}
		now := time.Now()
		if sess.RevokedAt != nil || now.After(sess.ExpiresAt) {
			s.ClearSessionCookie(w, r)
			next.ServeHTTP(w, r)
			return
		}

		if now.Sub(sess.LastSeenAt) >= 10*time.Minute {
			_ = s.users.TouchSession(r.Context(), sess.ID, now)
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

func (s *Service) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if CurrentUserFromRequest(r) == nil {
			if IsHtmx(r) {
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

func (s *Service) RequireGuest(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if CurrentUserFromRequest(r) != nil {
			http.Redirect(w, r, "/account", http.StatusSeeOther)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func CurrentUserFromRequest(r *http.Request) *user.User {
	if r == nil {
		return nil
	}
	if u, ok := r.Context().Value(currentUserContextKey).(*user.User); ok {
		return u
	}
	return nil
}

func ContextWithCurrentUser(ctx context.Context, currentUser *user.User) context.Context {
	return context.WithValue(ctx, currentUserContextKey, currentUser)
}

func (s *Service) CreateSession(ctx context.Context, currentUser user.User, meta RequestMeta) (string, time.Time, error) {
	rawToken, err := randomToken(32)
	if err != nil {
		return "", time.Time{}, err
	}
	expiresAt := time.Now().Add(s.sessionTTL)
	err = s.users.CreateSession(ctx, user.Session{
		UserID:     currentUser.ID,
		TokenHash:  HashToken(rawToken),
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

func HashToken(raw string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(raw)))
	return hex.EncodeToString(sum[:])
}

type RequestMeta struct {
	IP        string
	UserAgent string
}

func RequestMetaFromRequest(r *http.Request) RequestMeta {
	return RequestMeta{
		IP:        NormalizedClientIP(r),
		UserAgent: strings.TrimSpace(r.UserAgent()),
	}
}

func (s *Service) SetSessionCookie(w http.ResponseWriter, r *http.Request, token string, expiresAt time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     s.sessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   s.SessionCookieSecure(r),
		Expires:  expiresAt,
		MaxAge:   int(time.Until(expiresAt).Seconds()),
	})
}

func (s *Service) ClearSessionCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     s.sessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   s.SessionCookieSecure(r),
		MaxAge:   -1,
	})
}

func (s *Service) SessionTokenFromRequest(r *http.Request) string {
	if c, err := r.Cookie(s.sessionCookieName); err == nil {
		return strings.TrimSpace(c.Value)
	}
	return ""
}

func (s *Service) SessionCookieSecure(r *http.Request) bool {
	if s.sessionCookieForceSecure {
		return true
	}
	if strings.EqualFold(s.appEnv, "production") {
		return true
	}
	if r != nil && r.TLS != nil {
		return true
	}
	return r != nil && strings.EqualFold(strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")), "https")
}

func IsHtmx(r *http.Request) bool {
	return r.Header.Get("HX-Request") == "true"
}
