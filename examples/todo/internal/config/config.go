// Package config loads the to-do example's settings from defaults, an optional
// .env file and TODO_ environment variables.
package config

import (
	"slices"
	"strings"

	"github.com/borfast/gorfast/core/config"
	"github.com/borfast/gorfast/crypt"
)

const prefix = "TODO_"

// Config holds every setting. Crypt.Keys is a comma-separated list of key specs.
type Config struct {
	Server struct {
		Addr string `koanf:"addr"`
	} `koanf:"server"`
	Database struct {
		URL string `koanf:"url"`
	} `koanf:"database"`
	Crypt struct {
		Keys string `koanf:"keys"`
	} `koanf:"crypt"`
	Log struct {
		Level string `koanf:"level"`
	} `koanf:"log"`
}

// KeySpecs returns the key specs, trimmed, with empty entries left out.
func (c Config) KeySpecs() []string {
	var specs []string
	for _, spec := range strings.Split(c.Crypt.Keys, ",") {
		if spec = strings.TrimSpace(spec); spec != "" {
			specs = append(specs, spec)
		}
	}
	return specs
}

// Load reads and checks the configuration. Errors name variables, never values.
func Load(envFile string) (Config, error) {
	var cfg Config
	defaults := map[string]any{
		"server.addr":  "127.0.0.1:8080",
		"database.url": "todo.db",
		"log.level":    "info",
	}
	if err := config.Load(&cfg, prefix, defaults, envFile); err != nil {
		return Config{}, err
	}

	problems := config.NewProblems(prefix)
	problems.Require("server.addr", cfg.Server.Addr)
	problems.Require("database.url", cfg.Database.URL)
	specs := cfg.KeySpecs()
	if len(specs) == 0 {
		problems.Invalid("crypt.keys", "is required (make one with go run github.com/borfast/gorfast/crypt/cmd/newkey)")
	}
	for _, spec := range specs {
		if _, err := crypt.ParseKey(spec); err != nil {
			problems.Invalid("crypt.keys", "holds a key that does not parse")
			break
		}
	}
	if !slices.Contains([]string{"debug", "info", "warn", "error"}, cfg.Log.Level) {
		problems.Invalid("log.level", "must be debug, info, warn or error")
	}
	if err := problems.Err(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}
