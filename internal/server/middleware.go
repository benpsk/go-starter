package server

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"net/http"
	"strings"
)

const csrfCookieName = "csrf_token"

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "SAMEORIGIN")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		next.ServeHTTP(w, r)
	})
}

func csrfProtection(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			next.ServeHTTP(w, r)
			return
		}

		ensureCSRFCookie(w, r)

		if isSafeMethod(r.Method) {
			next.ServeHTTP(w, r)
			return
		}

		cookieToken := currentCSRFCookie(r)
		headerToken := strings.TrimSpace(r.Header.Get("X-CSRF-Token"))
		formToken := strings.TrimSpace(r.FormValue("csrf_token"))

		candidate := headerToken
		if candidate == "" {
			candidate = formToken
		}
		if !csrfTokensEqual(cookieToken, candidate) {
			http.Error(w, "invalid csrf token", http.StatusForbidden)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func isSafeMethod(method string) bool {
	switch strings.ToUpper(strings.TrimSpace(method)) {
	case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace:
		return true
	default:
		return false
	}
}

func ensureCSRFCookie(w http.ResponseWriter, r *http.Request) string {
	if token := currentCSRFCookie(r); token != "" {
		return token
	}

	token, err := newCSRFToken()
	if err != nil {
		return ""
	}

	http.SetCookie(w, &http.Cookie{
		Name:     csrfCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: false, // JS reads this to set htmx header and hidden form fields.
		SameSite: http.SameSiteLaxMode,
		Secure:   r.TLS != nil,
	})
	return token
}

func currentCSRFCookie(r *http.Request) string {
	c, err := r.Cookie(csrfCookieName)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(c.Value)
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
