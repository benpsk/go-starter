package web

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"net/http"
	"strings"
)

// csrfCookieName and csrfFieldName must match the names app.js reads and
// injects into forms.
const (
	csrfCookieName = "csrf_token"
	csrfFieldName  = "csrf_token"
	csrfHeaderName = "X-CSRF-Token"
)

// CSRFConfig captures the cookie settings for the CSRF token.
type CSRFConfig struct {
	Secure bool
}

// NewCSRFMiddleware returns double-submit CSRF protection for browser routes.
//
// This is web-only middleware: the JSON API authenticates with a bearer token
// rather than an ambient cookie, so it is not mounted there.
func NewCSRFMiddleware(cfg CSRFConfig) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ensureCSRFCookie(w, r, cfg.Secure)

			if isSafeMethod(r.Method) {
				next.ServeHTTP(w, r)
				return
			}

			cookieToken := csrfCookieValue(r)
			candidate := strings.TrimSpace(r.Header.Get(csrfHeaderName))
			if candidate == "" {
				candidate = strings.TrimSpace(r.FormValue(csrfFieldName))
			}
			if !csrfTokensEqual(cookieToken, candidate) {
				http.Error(w, "invalid csrf token", http.StatusForbidden)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func isSafeMethod(method string) bool {
	switch strings.ToUpper(strings.TrimSpace(method)) {
	case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace:
		return true
	default:
		return false
	}
}

func ensureCSRFCookie(w http.ResponseWriter, r *http.Request, secure bool) {
	if csrfCookieValue(r) != "" {
		return
	}
	token, err := newCSRFToken()
	if err != nil {
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     csrfCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: false, // app.js reads this to set the htmx header and form fields.
		SameSite: http.SameSiteLaxMode,
		Secure:   secure,
	})
}

func csrfCookieValue(r *http.Request) string {
	if r == nil {
		return ""
	}
	cookie, err := r.Cookie(csrfCookieName)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(cookie.Value)
}

func newCSRFToken() (string, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw[:]), nil
}

func csrfTokensEqual(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
