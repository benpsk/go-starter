package api

import (
	"mime"
	"net/http"

	"github.com/benpsk/go-starter/internal/apperror"
	"github.com/go-chi/chi/v5"
)

// Mount applies the common API contract, then registers feature routes.
func Mount(r chi.Router, mount func(chi.Router)) {
	r.Route("/api", func(apiRouter chi.Router) {
		apiRouter.Use(requireJSONContentType)
		apiRouter.NotFound(notFound)
		apiRouter.MethodNotAllowed(methodNotAllowed)
		mount(apiRouter)
	})
}

func requireJSONContentType(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body != nil && r.ContentLength != 0 {
			mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
			if err != nil || mediaType != "application/json" {
				Error(w, apperror.New(apperror.UnsupportedMediaType, "content type must be application/json", nil))
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func notFound(w http.ResponseWriter, _ *http.Request) {
	Error(w, apperror.NotFound("endpoint not found"))
}

func methodNotAllowed(w http.ResponseWriter, _ *http.Request) {
	Error(w, apperror.New(apperror.MethodNotAllowed, "method not allowed", nil))
}
