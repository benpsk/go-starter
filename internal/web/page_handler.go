package web

import (
	"net/http"

	"github.com/a-h/templ"
	"github.com/benpsk/go-starter/internal/auth"
	"github.com/benpsk/go-starter/internal/web/components"
	"github.com/benpsk/go-starter/internal/web/pages"
)

func (h Handler) homePage(w http.ResponseWriter, r *http.Request) {
	if auth.IsHtmx(r) {
		h.renderPage(w, r, components.Content(h.appName, pages.HomeContent()))
		return
	}
	h.renderPage(w, r, pages.HomePage(h.appName, h.appURL, h.googleTagID, h.headerAuthData(r)))
}

func (h Handler) aboutPage(w http.ResponseWriter, r *http.Request) {
	if auth.IsHtmx(r) {
		h.renderPage(w, r, components.Content("About | "+h.appName, pages.AboutContent()))
		return
	}
	h.renderPage(w, r, pages.AboutPage(h.appName, h.appURL, h.googleTagID, h.headerAuthData(r)))
}

func (h Handler) notFoundPage(w http.ResponseWriter, r *http.Request) {
	if auth.IsHtmx(r) {
		h.renderPageStatus(w, r, http.StatusNotFound, components.Content("Not Found | "+h.appName, pages.NotFoundContent()))
		return
	}
	h.renderPageStatus(w, r, http.StatusNotFound, pages.NotFoundPage(h.appName, h.appURL, h.googleTagID, h.headerAuthData(r)))
}

func (h Handler) methodNotAllowedPage(w http.ResponseWriter, r *http.Request) {
	if auth.IsHtmx(r) {
		h.renderPageStatus(w, r, http.StatusMethodNotAllowed, components.Content("Method Not Allowed | "+h.appName, pages.MethodNotAllowedContent()))
		return
	}
	h.renderPageStatus(w, r, http.StatusMethodNotAllowed, pages.MethodNotAllowedPage(h.appName, h.appURL, h.googleTagID, h.headerAuthData(r)))
}

func (h Handler) renderPage(w http.ResponseWriter, r *http.Request, component templ.Component) {
	h.renderPageStatus(w, r, http.StatusOK, component)
}

func (h Handler) renderPageStatus(w http.ResponseWriter, r *http.Request, status int, component templ.Component) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := component.Render(r.Context(), w); err != nil {
		http.Error(w, "failed to render page", http.StatusInternalServerError)
	}
}

func (h Handler) headerAuthData(r *http.Request) components.HeaderAuthData {
	currentUser := auth.CurrentUserFromRequest(r)
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
