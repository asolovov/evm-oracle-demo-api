// Package api owns the public REST description. The OpenAPI YAML in this
// directory is the canonical contract surface; this package embeds it so the
// service binary ships with a copy and the `/api/v1/openapi.yaml` +
// `/api/v1/docs` endpoints work without external file dependencies.
package api

import _ "embed"

// OpenAPISpec is the embedded OpenAPI 3.1 description of the /api/v1 surface.
//
//go:embed openapi.yaml
var OpenAPISpec []byte
