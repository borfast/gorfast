# Koanf Configuration: Complete Implementation

Full working code for the layered configuration pattern described in SKILL.md.
Copy `config.go` as-is and adapt the structs to your application.

- [Dependencies](#dependencies)
- [config.go](#configgo)
- [Wiring it into main.go](#wiring-it-into-maingo)
- [.env.dist template](#envdist-template)
- [.gitignore patterns](#gitignore-patterns)

## Dependencies

```bash
go get github.com/knadh/koanf/v2
go get github.com/knadh/koanf/providers/confmap
go get github.com/knadh/koanf/providers/env/v2
go get github.com/knadh/koanf/providers/file
go get github.com/knadh/koanf/parsers/dotenv
```

The env provider is `providers/env/v2`. Its API differs from v1: v2 takes
`Provider(delim string, opt env.Opt)`, while v1 took
`Provider(prefix, delim string, cb func(string) string)`. Mixing the v1 call
shape with the v2 import will not compile.

## config.go

Create `internal/config/config.go`:

```go
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"time"

	"github.com/knadh/koanf/parsers/dotenv"
	"github.com/knadh/koanf/providers/confmap"
	"github.com/knadh/koanf/providers/env/v2"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
)

// EnvPrefix is the prefix for all environment variables.
// Change this to match your application name (e.g., "MYAPP_", "GORFAST_").
const EnvPrefix = "MYAPP_"

// Config holds all application configuration.
type Config struct {
	Server   ServerConfig   `koanf:"server"`
	Database DatabaseConfig `koanf:"database"`
	Log      LogConfig      `koanf:"log"`
}

type ServerConfig struct {
	Host         string        `koanf:"host"`
	Port         int           `koanf:"port"`
	ReadTimeout  time.Duration `koanf:"read_timeout"`
	WriteTimeout time.Duration `koanf:"write_timeout"`
}

type DatabaseConfig struct {
	URL      string `koanf:"url"`
	MaxConns int    `koanf:"max_conns"`
}

type LogConfig struct {
	Level  string `koanf:"level"`
	Format string `koanf:"format"`
}

// defaults holds default configuration values as a flat map.
// Keys use dot notation matching koanf paths.
var defaults = map[string]any{
	"server.host":          "localhost",
	"server.port":          8080,
	"server.read_timeout":  30 * time.Second,
	"server.write_timeout": 30 * time.Second,
	"database.max_conns":   10,
	"log.level":            "info",
	"log.format":           "json",
}

// Load reads configuration from defaults, .env file, and environment variables.
// Later sources override earlier ones: defaults < .env < environment variables.
func Load() (*Config, error) {
	k := koanf.New(".")

	// 1. Load defaults first (lowest priority)
	if err := k.Load(confmap.Provider(defaults, "."), nil); err != nil {
		return nil, fmt.Errorf("loading defaults: %w", err)
	}

	// 2. Load .env file (overrides defaults, optional)
	if err := k.Load(file.Provider(".env"), dotenv.ParserEnv(EnvPrefix, ".", transformKey)); err != nil {
		// .env file is optional - only fail on parse errors, not missing file
		if !errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("loading .env file: %w", err)
		}
	}

	// 3. Load environment variables (highest priority, overrides all)
	if err := k.Load(env.Provider(".", env.Opt{
		Prefix:        EnvPrefix,
		TransformFunc: func(k, v string) (string, any) { return transformKey(k), v },
	}), nil); err != nil {
		return nil, fmt.Errorf("loading environment variables: %w", err)
	}

	var cfg Config
	if err := k.Unmarshal("", &cfg); err != nil {
		return nil, fmt.Errorf("unmarshaling config: %w", err)
	}

	if err := validate(&cfg); err != nil {
		return nil, fmt.Errorf("validating config: %w", err)
	}

	return &cfg, nil
}

// transformKey converts environment variable names to koanf paths.
// It uses double underscores (__) as the nesting separator.
// Example: MYAPP_SERVER__HOST -> server.host
// Example: MYAPP_DATABASE__MAX_CONNS -> database.max_conns
func transformKey(s string) string {
	// Remove prefix and lowercase
	s = strings.ToLower(strings.TrimPrefix(s, EnvPrefix))
	// Replace double underscores with dots for nesting
	s = strings.Replace(s, "__", ".", -1)
	return s
}

// validate checks that all required configuration fields are set.
func validate(cfg *Config) error {
	var errs []string

	if cfg.Database.URL == "" {
		errs = append(errs, "database.url is required (set "+EnvPrefix+"DATABASE__URL)")
	}

	if len(errs) > 0 {
		return fmt.Errorf("missing required config: %s", strings.Join(errs, "; "))
	}

	return nil
}
```

Both `dotenv.ParserEnv` and the env provider hand the callback the *full*
variable name including the prefix, which is why `transformKey` trims it.

## Wiring it into main.go

```go
package main

import (
	"log"

	"yourmodule/internal/config"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	log.Printf("Starting server on %s:%d", cfg.Server.Host, cfg.Server.Port)
	// Use cfg throughout your application
}
```

## .env.dist template

Create `.env.dist` as a documented example, and commit it:

```bash
# Application Configuration Template
# Copy to .env and fill in values for local development
# Environment variables override these values in production
# Use double underscores (__) to separate sections

# Server Configuration
MYAPP_SERVER__HOST=localhost
MYAPP_SERVER__PORT=8080
MYAPP_SERVER__READ_TIMEOUT=30s
MYAPP_SERVER__WRITE_TIMEOUT=30s

# Database Configuration (REQUIRED)
MYAPP_DATABASE__URL=postgres://user:pass@localhost:5432/dbname?sslmode=disable
MYAPP_DATABASE__MAX_CONNS=10

# Logging Configuration
MYAPP_LOG__LEVEL=info
MYAPP_LOG__FORMAT=json
```

## .gitignore patterns

```gitignore
# Environment files with secrets
.env
.env.local
.env.*.local

# Keep the template
!.env.dist
```
