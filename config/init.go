// Package config defines application configuration defaults and schema.
package config

import "github.com/spf13/viper"

//nolint:gochecknoinits // configuration defaults are registered at package load.
func init() {
	setDefaults()
}

// setDefaults exposes default registration for testing.
// Real per-field defaults land in the config task that follows the template prune.
func setDefaults() {
	viper.SetDefault("env", "prod")
}
