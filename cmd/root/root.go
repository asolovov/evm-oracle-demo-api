// Package root defines the root CLI command.
package root

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/go-viper/mapstructure/v2"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"

	"github.com/asolovov/evm-oracle-demo-api/config"
	"github.com/asolovov/evm-oracle-demo-api/internal"
)

// Cmd returns the root command for the application.
func Cmd(app *internal.App) *cobra.Command {
	cmd := &cobra.Command{
		Use:              "evm-oracle-demo-api",
		Short:            "EVM Oracle Demo - REST + WebSocket BFF over the oracle gRPC plane",
		TraverseChildren: true,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			return initializeConfig(cmd, app.Config())
		},
	}

	cmd.Version = app.Version()
	cmd.SetVersionTemplate("{{printf \"%s\" .Version}}\n")

	return cmd
}

// initializeConfig reads in config file and sets configuration via environment variables.
// Env and flags are bound after config load so CLI flags override env, which override config file.
func initializeConfig(cmd *cobra.Command, cfg *config.Scheme) error {
	if err := viper.ReadInConfig(); err != nil {
		var configFileNotFound viper.ConfigFileNotFoundError
		if !errors.As(err, &configFileNotFound) {
			return fmt.Errorf("read config file: %w", err)
		}
	}

	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	viper.AutomaticEnv()
	viper.AllowEmptyEnv(true)

	bindFlags(cmd)

	if err := viper.BindPFlags(cmd.Flags()); err != nil {
		return fmt.Errorf("bind flags: %w", err)
	}

	// Compose our JSON-string->map hook with viper's defaults so map-typed
	// env vars (e.g. AUTHOR_LINKS='{"github":"..."}') decode, while the
	// default duration + comma-slice hooks that env []string fields rely on
	// (CORS_ORIGINS, TRUSTED_PROXIES, SUBSCRIBE_ASSET_IDS) keep working.
	return viper.Unmarshal(cfg, viper.DecodeHook(mapstructure.ComposeDecodeHookFunc(
		mapstructure.StringToTimeDurationHookFunc(),
		mapstructure.StringToSliceHookFunc(","),
		stringToStringMapJSONHook(),
	)))
}

// stringToStringMapJSONHook decodes a JSON-object string into a
// map[string]string. viper.AutomaticEnv delivers every env var as a string,
// so a map-typed field set from the environment arrives as raw JSON text
// rather than a map — without this hook viper.Unmarshal errors with
// "expected type 'map[string]string', got unconvertible type 'string'".
// Only fires for string -> map[string]string; every other conversion is
// untouched.
func stringToStringMapJSONHook() mapstructure.DecodeHookFuncType {
	return func(from, to reflect.Type, data interface{}) (interface{}, error) {
		if from.Kind() != reflect.String || to != reflect.TypeOf(map[string]string{}) {
			return data, nil
		}
		raw, _ := data.(string)
		if strings.TrimSpace(raw) == "" {
			return map[string]string{}, nil
		}
		out := map[string]string{}
		if err := json.Unmarshal([]byte(raw), &out); err != nil {
			return nil, fmt.Errorf("decode map-typed config from JSON string %q: %w", raw, err)
		}
		return out, nil
	}
}

// bindFlags binds flags to the command.
func bindFlags(cmd *cobra.Command) {
	cmd.Flags().VisitAll(func(f *pflag.Flag) {
		if !f.Changed && viper.IsSet(f.Name) {
			val := viper.Get(f.Name)
			_ = cmd.Flags().Set(f.Name, fmt.Sprintf("%v", val))
		}
	})
}
