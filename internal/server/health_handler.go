package server

import (
	"encoding/json"
	"net/http"
	"time"

	healthsvc "github.com/benpsk/go-starter/internal/health"
)

// HealthHandler exposes the liveness and readiness endpoints.
type HealthHandler struct {
	svc healthsvc.Service
}

// NewHealthHandler wires a health service into a handler instance.
func NewHealthHandler(svc healthsvc.Service) HealthHandler {
	return HealthHandler{svc: svc}
}

// Live answers whether the process is running, without touching dependencies.
func (h HealthHandler) Live(w http.ResponseWriter, _ *http.Request) {
	h.writeStatus(w, http.StatusOK, healthsvc.Status{
		Status:    healthsvc.StatusOK,
		CheckedAt: time.Now().UTC(),
		Database: healthsvc.DatabaseDetails{
			Status:  healthsvc.StatusSkipped,
			Message: "readiness-only check",
		},
	})
}

// Ready answers whether the process can serve traffic, dependencies included.
func (h HealthHandler) Ready(w http.ResponseWriter, r *http.Request) {
	if h.svc == nil {
		h.writeStatus(w, http.StatusServiceUnavailable, healthsvc.Status{
			Status:    healthsvc.StatusDegraded,
			CheckedAt: time.Now().UTC(),
			Database: healthsvc.DatabaseDetails{
				Status:  healthsvc.StatusSkipped,
				Message: "health service not initialised",
			},
		})
		return
	}

	status, err := h.svc.Status(r.Context())
	if err != nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	statusCode := http.StatusOK
	if status.Status != healthsvc.StatusOK {
		statusCode = http.StatusServiceUnavailable
	}
	h.writeStatus(w, statusCode, status)
}

func (h HealthHandler) writeStatus(w http.ResponseWriter, statusCode int, status healthsvc.Status) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(statusCode)
	if err := json.NewEncoder(w).Encode(status); err != nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
	}
}
