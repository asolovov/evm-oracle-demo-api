package handlers

import (
	"net/http"

	"github.com/asolovov/evm-oracle-demo-api/api"
)

const swaggerUIVersion = "5.17.14"

// swaggerUIHTML is the Swagger UI shell. Loads the JS + CSS bundles from
// unpkg pinned to a specific version so the docs render consistently across
// CDN cache state. The spec URL is relative to the BFF, not pulled from the
// CDN.
const swaggerUIHTML = `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="utf-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1" />
    <title>EVM Oracle Demo API — Swagger UI</title>
    <link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@` + swaggerUIVersion + `/swagger-ui.css" />
    <link rel="icon" type="image/png" href="https://unpkg.com/swagger-ui-dist@` + swaggerUIVersion + `/favicon-32x32.png" sizes="32x32" />
    <style>
        html { box-sizing: border-box; overflow-y: scroll; }
        *, *:before, *:after { box-sizing: inherit; }
        body { margin: 0; background: #fafafa; }
    </style>
</head>
<body>
    <div id="swagger-ui"></div>
    <script src="https://unpkg.com/swagger-ui-dist@` + swaggerUIVersion + `/swagger-ui-bundle.js" crossorigin></script>
    <script src="https://unpkg.com/swagger-ui-dist@` + swaggerUIVersion + `/swagger-ui-standalone-preset.js" crossorigin></script>
    <script>
        window.onload = function () {
            window.ui = SwaggerUIBundle({
                url: "/api/v1/openapi.yaml",
                dom_id: "#swagger-ui",
                deepLinking: true,
                presets: [
                    SwaggerUIBundle.presets.apis,
                    SwaggerUIStandalonePreset
                ],
                plugins: [
                    SwaggerUIBundle.plugins.DownloadUrl
                ],
                layout: "StandaloneLayout"
            });
        };
    </script>
</body>
</html>
`

// Docs serves the Swagger UI HTML shell at GET /api/v1/docs. The HTML pulls
// the spec from /api/v1/openapi.yaml.
func (a *API) Docs(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// Same-origin only — the spec is served by this BFF.
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write([]byte(swaggerUIHTML))
}

// OpenAPISpec serves the embedded YAML at GET /api/v1/openapi.yaml.
func (a *API) OpenAPISpec(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(api.OpenAPISpec)
}
