package handlers

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/asolovov/evm-oracle-demo-api/internal/middleware"
)

// Register mounts the /api/v1/* surface onto the supplied chi router.
// Middleware ordering (outermost first): request-id, access log, recovery,
// any global middleware (e.g. the HTTP metrics middleware), CORS, then any
// apiMiddleware (rate-limit etc.) applied to the /api/v1 sub-route only —
// WebSocket and health are not rate-limited.
func (a *API) Register(r chi.Router, corsMW func(http.Handler) http.Handler, apiMiddleware ...func(http.Handler) http.Handler) {
	r.Use(middleware.RequestID())
	r.Use(middleware.AccessLog())
	r.Use(middleware.Recovery())
	for _, mw := range a.GlobalMiddleware {
		r.Use(mw)
	}
	r.Use(corsMW)

	r.Route("/api/v1", func(r chi.Router) {
		for _, mw := range apiMiddleware {
			r.Use(mw)
		}
		r.Get("/health", a.Health)
		r.Get("/assets", a.ListAssets)
		r.Get("/assets/{id}/price", a.GetAssetPrice)
		r.Get("/assets/{id}/history", a.GetAssetHistory)
		r.Get("/requests/{reqId}", a.GetRequest)
		r.Post("/requests/build-tx", a.BuildTx)
	})
}

