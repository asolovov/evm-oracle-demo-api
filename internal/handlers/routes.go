package handlers

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/asolovov/evm-oracle-demo-api/internal/middleware"
)

// Register mounts the /api/v1/* surface onto the supplied chi router.
// Middleware ordering (outermost first): request-id, access log, recovery,
// CORS. The recovery middleware sits below the access log so a panicked
// handler still leaves an access log line with status=500.
func (a *API) Register(r chi.Router, corsMW func(http.Handler) http.Handler) {
	r.Use(middleware.RequestID())
	r.Use(middleware.AccessLog())
	r.Use(middleware.Recovery())
	r.Use(corsMW)

	r.Route("/api/v1", func(r chi.Router) {
		r.Get("/health", a.Health)
		r.Get("/assets", a.ListAssets)
		r.Get("/assets/{id}/price", a.GetAssetPrice)
		r.Get("/assets/{id}/history", a.GetAssetHistory)
		r.Get("/requests/{reqId}", a.GetRequest)
		r.Post("/requests/build-tx", a.BuildTx)
	})
}

