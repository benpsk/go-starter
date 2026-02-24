package server

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/a-h/templ"
	"github.com/benpsk/go-starter/internal/web/components"
	"github.com/benpsk/go-starter/internal/web/pages"
	"github.com/jackc/pgx/v5/pgxpool"
)

type handler struct {
	db      *pgxpool.Pool
	appName string
	appURL  string
}

func newHandler(db *pgxpool.Pool, appName, appURL string) handler {
	return handler{db: db, appName: appName, appURL: appURL}
}

func (h handler) homePage(w http.ResponseWriter, r *http.Request) {
	if isHtmx(r) {
		h.renderPage(w, r, components.Content(h.appName, pages.HomeContent()))
		return
	}
	h.renderPage(w, r, pages.HomePage(h.appName, h.appURL))
}

func (h handler) aboutPage(w http.ResponseWriter, r *http.Request) {
	if isHtmx(r) {
		h.renderPage(w, r, components.Content("About | "+h.appName, pages.AboutContent()))
		return
	}
	h.renderPage(w, r, pages.AboutPage(h.appName, h.appURL))
}

func (h handler) healthz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	payload := map[string]any{"status": "ok", "database": "up"}
	status := http.StatusOK

	if h.db != nil {
		if err := h.db.Ping(ctx); err != nil {
			payload["status"] = "degraded"
			payload["database"] = err.Error()
			status = http.StatusServiceUnavailable
		}
	}

	writeJSON(w, status, payload)
}

func isHtmx(r *http.Request) bool {
	return r.Header.Get("HX-Request") == "true"
}

func (h handler) renderPage(w http.ResponseWriter, r *http.Request, component templ.Component) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := component.Render(r.Context(), w); err != nil {
		http.Error(w, "failed to render page", http.StatusInternalServerError)
	}
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if payload == nil {
		return
	}
	_ = json.NewEncoder(w).Encode(payload)
}
