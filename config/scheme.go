// Package config defines application configuration defaults and schema.
package config

// Scheme is the placeholder application configuration scheme.
// Real fields land in the config task that follows the template prune.
type Scheme struct {
	Env string `mapstructure:"env"`
}
