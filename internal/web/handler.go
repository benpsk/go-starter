package web

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"github.com/a-h/templ"
	"github.com/benpsk/go-starter/internal/apperror"
	"github.com/benpsk/go-starter/internal/authctx"
	"github.com/benpsk/go-starter/internal/config"
	"github.com/benpsk/go-starter/internal/web/components"
)

// IsHtmx reports whether htmx issued the request, so a handler can answer with
// a fragment instead of a full page.
func IsHtmx(r *http.Request) bool {
	return r != nil && r.Header.Get("HX-Request") == "true"
}

// Error writes err as a JSON error envelope. Browser handlers that render a
// page use redirects or an error page instead; this is for htmx fragments and
// form endpoints that answer with JSON.
func Error(w http.ResponseWriter, err error) {
	var appErr *apperror.AppError

	if errors.As(err, &appErr) {
		if appErr.Err != nil {
			log.Printf("web: internal error: %v", appErr.Err)
		}
		body := make(map[string]any)
		if appErr.Details != nil {
			body["errors"] = appErr.Details
		} else {
			body["errors"] = map[string]string{"message": appErr.Message}
		}
		toJSON(w, appErr.Status, body)
		return
	}

	log.Printf("web: unexpected error: %v", err)
	toJSON(w, http.StatusInternalServerError, map[string]any{
		"errors": map[string]string{"message": "an unexpected internal error occurred"},
	})
}

// Success writes payload as a JSON success envelope.
func Success(w http.ResponseWriter, code int, payload any) {
	switch value := payload.(type) {
	case string:
		toJSON(w, code, map[string]any{"message": value})
	default:
		toJSON(w, code, map[string]any{"data": payload})
	}
}

func toJSON(w http.ResponseWriter, code int, payload any) {
	body, err := json.Marshal(payload)
	if err != nil {
		log.Printf("web: could not marshal json response: %v", err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_, _ = w.Write(body)
}

// render writes a templ component as a 200 HTML response.
func render(w http.ResponseWriter, r *http.Request, component templ.Component) {
	renderStatus(w, r, http.StatusOK, component)
}

// renderStatus writes a templ component with an explicit status code.
func renderStatus(w http.ResponseWriter, r *http.Request, status int, component templ.Component) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := component.Render(r.Context(), w); err != nil {
		log.Printf("web: render failed: %v", err)
	}
}

// pageProps assembles the base props every page needs: the render-time
// settings, the page metadata, and the header's view of the current session.
func pageProps(setting config.Setting, r *http.Request, meta components.Meta) components.PageProps {
	return components.PageProps{
		Setting: setting,
		Meta:    meta,
		Auth:    headerAuthProps(r),
	}
}

func headerAuthProps(r *http.Request) components.HeaderAuthProps {
	if r == nil {
		return components.HeaderAuthProps{}
	}
	principal, ok := authctx.UserFromContext(r.Context())
	if !ok {
		return components.HeaderAuthProps{}
	}
	name := principal.DisplayName
	if name == "" {
		name = "Account"
	}
	return components.HeaderAuthProps{
		IsAuthenticated: true,
		DisplayName:     name,
		AvatarURL:       principal.AvatarURL,
	}
}

// isConflict reports whether err is a 409, which the sign-in flow surfaces as
// an account-linking message rather than a generic failure.
func isConflict(err error) bool {
	var appErr *apperror.AppError
	return errors.As(err, &appErr) && appErr.Status == http.StatusConflict
}
