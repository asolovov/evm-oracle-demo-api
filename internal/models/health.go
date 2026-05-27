package models

// HealthResponse is the body returned by GET /api/v1/health. The author
// block surfaces the credentials the portfolio site requires (FR-09).
type HealthResponse struct {
	Status  string         `json:"status"`
	Service string         `json:"service"`
	Version string         `json:"version"`
	Author  AuthorResponse `json:"author"`
}

// AuthorResponse carries the credential link map.
type AuthorResponse struct {
	Name  string            `json:"name"`
	Links map[string]string `json:"links,omitempty"`
}
