---
name: gorfast-config
description: Use when adding or editing Go configuration code - generates Koanf-based configuration with environment variables, .env file support, app-prefixed env vars, typed structs, and validation
---

# Go Configuration with Koanf

## Overview

Use the [Koanf](https://github.com/knadh/koanf) library instead of manual `os.Getenv` patterns for configuration. Benefits:

- **Type safety** - Unmarshal directly into typed structs
- **Layered config** - Defaults → .env files → environment variables (later overrides earlier)
- **Automatic unmarshaling** - No manual string parsing for ints, bools, durations
- **Validation** - Centralized required field checking
- **Testability** - Easy to inject test configurations

## When to Use This Skill

- Creating a new config package
- Adding configuration fields to an existing app
- Setting up .env file support
- Migrating from `os.Getenv` to structured config

## Quick Reference

### Dependencies

```bash
go get github.com/knadh/koanf/v2
go get github.com/knadh/koanf/providers/confmap
go get github.com/knadh/koanf/providers/env
go get github.com/knadh/koanf/providers/file
go get github.com/knadh/koanf/parsers/dotenv
```

### Struct Tags

Use `koanf` tags matching the dot-notation path:

```go
type ServerConfig struct {
    Host string `koanf:"host"`
    Port int    `koanf:"port"`
}
```

### Environment Variable Naming

With prefix `MYAPP_` and **double underscores (`__`) as separators**:

| Env Variable | Koanf Path | Struct Field |
|--------------|------------|--------------|
| MYAPP_SERVER__HOST | server.host | Config.Server.Host |
| MYAPP_SERVER__PORT | server.port | Config.Server.Port |
| MYAPP_DATABASE__URL | database.url | Config.Database.URL |
| MYAPP_DATABASE__MAX_CONNS | database.max_conns | Config.Database.MaxConns |
| MYAPP_LOG__LEVEL | log.level | Config.Log.Level |

**Pattern:** `PREFIX_SECTION__FIELD` → `section.field`
- **Double Underscore (`__`)**: Becomes a dot (`.`) for nesting.
- **Single Underscore (`_`)**: Preserved (e.g., `MAX_CONNS` -> `max_conns`).

## Complete Implementation

Create `internal/config/config.go`:

```go
package config

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/knadh/koanf/parsers/dotenv"
	"github.com/knadh/koanf/providers/confmap"
	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
)

// EnvPrefix is the prefix for all environment variables.
// Change this to match your application name (e.g., "MYAPP_", "GORFAST_").
const EnvPrefix = "MYAPP_"

// Config holds all application configuration.
type Config struct {
	Server   ServerConfig
	Database DatabaseConfig
	Log      LogConfig
}

type ServerConfig struct {
	Host         string        `koanf:"host"`
	Port         int           `koanf:"port"`
	ReadTimeout  time.Duration `koanf:"readtimeout"`
	WriteTimeout time.Duration `koanf:"writetimeout"`
}

type DatabaseConfig struct {
	URL      string `koanf:"url"`
	MaxConns int    `koanf:"maxconns"`
}

type LogConfig struct {
	Level  string `koanf:"level"`
	Format string `koanf:"format"`
}

// defaults holds default configuration values as a flat map.
// Keys use dot notation matching koanf paths.
var defaults = map[string]any{
	"server.host":         "localhost",
	"server.port":         8080,
	"server.readtimeout":  30 * time.Second,
	"server.writetimeout": 30 * time.Second,
	"database.maxconns":   10,
	"log.level":           "info",
	"log.format":          "json",
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
		if !errors.Is(err, file.ErrNotExists) {
			return nil, fmt.Errorf("loading .env file: %w", err)
		}
	}

	// 3. Load environment variables (highest priority, overrides all)
	if err := k.Load(env.Provider(EnvPrefix, ".", transformKey), nil); err != nil {
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

### Usage in main.go

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

## .env.dist Template

Create `.env.dist` as a documented example (commit this file):

```bash
# Application Configuration Template
# Copy to .env and fill in values for local development
# Environment variables override these values in production
# Use double underscores (__) to separate sections

# Server Configuration
MYAPP_SERVER__HOST=localhost
MYAPP_SERVER__PORT=8080
MYAPP_SERVER__READTIMEOUT=30s
MYAPP_SERVER__WRITETIMEOUT=30s

# Database Configuration (REQUIRED)
MYAPP_DATABASE__URL=postgres://user:pass@localhost:5432/dbname?sslmode=disable
MYAPP_DATABASE__MAXCONNS=10

# Logging Configuration
MYAPP_LOG__LEVEL=info
MYAPP_LOG__FORMAT=json
```

## .gitignore Patterns

Add to `.gitignore`:

```gitignore
# Environment files with secrets
.env
.env.local
.env.*.local

# Keep the template
!.env.dist
```

## Adding New Configuration Fields

1. **Add struct field** with `koanf` tag:
   ```go
   type Config struct {
       // ... existing fields
       Redis RedisConfig
   }

   type RedisConfig struct {
       URL string `koanf:"url"`
   }
   ```

2. **Add default** to `defaults` map if optional:
   ```go
   var defaults = map[string]any{
       // ... existing defaults
       "redis.url": "localhost:6379",
   }
   ```

3. **Add validation** in `validate()` if required:
   ```go
   if cfg.Redis.URL == "" {
       errs = append(errs, "redis.url is required (set "+EnvPrefix+"REDIS__URL)")
   }
   ```

4. **Update .env.dist** with documentation:
   ```bash
   # Redis Configuration
   MYAPP_REDIS__URL=localhost:6379
   ```

5. **Set environment variable** following the naming convention:
   - Struct path: `Config.Redis.URL`
   - Env var: `MYAPP_REDIS__URL`

## Common Mistakes

### Case Sensitivity

Environment variables are case-sensitive. Always use UPPERCASE for env vars:
- Correct: `MYAPP_DATABASE__URL`
- Wrong: `myapp_database__url`

### Prefix Must Match

The `EnvPrefix` constant must exactly match your environment variable prefix:
- If using `MYAPP_DATABASE__URL`, set `EnvPrefix = "MYAPP_"`
- If using `APP_DATABASE__URL`, set `EnvPrefix = "APP_"`

### Double vs Single Underscore

Use **double underscores (`__`)** to indicate nesting (dots), and **single underscores (`_`)** for word separation.

- `MYAPP_DATABASE__MAX_CONNS` → `database.max_conns` (Correct for `max_conns` field)
- `MYAPP_DATABASE_MAX_CONNS` → `database_max_conns` (Treated as a top-level field `database_max_conns`)
- `MYAPP_DATABASE__URL` → `database.url`

### Duration Parsing

Koanf can parse duration strings like `30s`, `5m`, `1h`:

```go
ReadTimeout time.Duration `koanf:"readtimeout"`
```

Set via: `MYAPP_SERVER__READTIMEOUT=30s`

## Anti-Patterns

### Global Koanf Instance

Don't keep a global `*koanf.Koanf` instance. Load once and pass the typed `*Config` struct:

```go
// Bad - global mutable state
var k = koanf.New(".")

// Good - load once, pass typed config
cfg, err := config.Load()
server.New(cfg)
```

### Loading Multiple Times

Load configuration once at startup, not on every request:

```go
// Bad - loading on every call
func GetDBURL() string {
    cfg, _ := config.Load()
    return cfg.Database.URL
}

// Good - load once, inject where needed
type Server struct {
    cfg *config.Config
}
```

### Stringly-Typed Access

Don't use `k.String("database.url")` throughout your code. Unmarshal to structs:

```go
// Bad - string keys everywhere, no type safety
url := k.String("database.url")
port := k.Int("server.port")

// Good - typed struct access
cfg.Database.URL
cfg.Server.Port
```

## Quality Checklist

Before completing configuration code:

- [ ] All struct fields have `koanf` tags
- [ ] Required fields are validated in `validate()`
- [ ] Optional fields have defaults in `defaults` map
- [ ] Default keys in `defaults` match koanf paths (e.g., `"server.host"`)
- [ ] `.env.dist` documents all variables with examples
- [ ] `.gitignore` excludes `.env` but not `.env.dist`
- [ ] `EnvPrefix` matches your application name
- [ ] Environment variable names follow `PREFIX_SECTION__FIELD` pattern (Double Underscore)
- [ ] Config is loaded once at startup and passed to components
- [ ] No global koanf instance or repeated loading
