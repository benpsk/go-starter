package web

import (
	"net/http"

	"github.com/benpsk/go-starter/internal/config"
	"github.com/benpsk/go-starter/internal/web/components"
	"github.com/benpsk/go-starter/internal/web/pages"
)

// PageHandler renders the static marketing and error pages.
type PageHandler struct {
	setting config.Setting
}

// NewPageHandler wires the render settings into a handler instance.
func NewPageHandler(setting config.Setting) PageHandler {
	return PageHandler{setting: setting}
}

// Home renders the landing page.
func (h PageHandler) Home(w http.ResponseWriter, r *http.Request) {
	props := pageProps(h.setting, r, components.Meta{
		Title:       h.setting.AppName,
		Description: "Minimal Go starter with chi, pgx, templ, Tailwind, DaisyUI, htmx, and Chart.js.",
		Keywords:    "go starter,chi,pgx,templ,tailwind,daisyui,htmx,chartjs",
		Path:        "/",
	})
	if IsHtmx(r) {
		render(w, r, components.Content(props.FullTitle(), pages.HomeContent()))
		return
	}
	render(w, r, pages.HomePage(props))
}

// About renders the about page.
func (h PageHandler) About(w http.ResponseWriter, r *http.Request) {
	props := pageProps(h.setting, r, components.Meta{
		Title:       "About",
		Description: "About the Go Starter template and the structure it follows.",
		Keywords:    "go starter about",
		Path:        "/about",
		Type:        "article",
	})
	if IsHtmx(r) {
		render(w, r, components.Content(props.FullTitle(), pages.AboutContent()))
		return
	}
	render(w, r, pages.AboutPage(props))
}

// NotFound renders the 404 page.
func (h PageHandler) NotFound(w http.ResponseWriter, r *http.Request) {
	props := pageProps(h.setting, r, components.Meta{
		Title:       "Page Not Found",
		Description: "The requested page could not be found.",
		Keywords:    "404,not found",
		Path:        "/404",
	})
	if IsHtmx(r) {
		renderStatus(w, r, http.StatusNotFound, components.Content(props.FullTitle(), pages.NotFoundContent()))
		return
	}
	renderStatus(w, r, http.StatusNotFound, pages.NotFoundPage(props))
}

// MethodNotAllowed renders the 405 page.
func (h PageHandler) MethodNotAllowed(w http.ResponseWriter, r *http.Request) {
	props := pageProps(h.setting, r, components.Meta{
		Title:       "Method Not Allowed",
		Description: "The HTTP method is not allowed for this route.",
		Keywords:    "405,method not allowed",
		Path:        "/405",
	})
	if IsHtmx(r) {
		renderStatus(w, r, http.StatusMethodNotAllowed, components.Content(props.FullTitle(), pages.MethodNotAllowedContent()))
		return
	}
	renderStatus(w, r, http.StatusMethodNotAllowed, pages.MethodNotAllowedPage(props))
}
