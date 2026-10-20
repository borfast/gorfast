package config_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/borfast/gorfast/core/config"
)

type testConfig struct {
	Server struct {
		Addr        string        `koanf:"addr"`
		ReadTimeout time.Duration `koanf:"read_timeout"`
	} `koanf:"server"`
	Name string `koanf:"name"`
}

func testDefaults() map[string]any {
	return map[string]any{
		"server.addr":         "a",
		"server.read_timeout": time.Second,
		"name":                "d",
	}
}

func TestTransformKey(t *testing.T) {
	tests := []struct{ name, want string }{
		{"TODO_SERVER__READ_TIMEOUT", "server.read_timeout"},
		{"TODO_NAME", "name"},
	}
	for _, tt := range tests {
		if got := config.TransformKey("TODO_", tt.name); got != tt.want {
			t.Errorf("TransformKey(%q) = %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestVariable(t *testing.T) {
	tests := []struct{ key, want string }{
		{"server.read_timeout", "TODO_SERVER__READ_TIMEOUT"},
		{"name", "TODO_NAME"},
	}
	for _, tt := range tests {
		if got := config.Variable("TODO_", tt.key); got != tt.want {
			t.Errorf("Variable(%q) = %q, want %q", tt.key, got, tt.want)
		}
	}
}

func TestLoadLayers(t *testing.T) {
	envFile := filepath.Join(t.TempDir(), ".env")
	content := "TODO_SERVER__ADDR=b\nTODO_NAME=e\n"
	if err := os.WriteFile(envFile, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TODO_NAME", "f")

	var cfg testConfig
	if err := config.Load(&cfg, "TODO_", testDefaults(), envFile); err != nil {
		t.Fatal(err)
	}
	if cfg.Server.Addr != "b" {
		t.Errorf("Addr = %q, want b", cfg.Server.Addr)
	}
	if cfg.Server.ReadTimeout != time.Second {
		t.Errorf("ReadTimeout = %v, want 1s", cfg.Server.ReadTimeout)
	}
	if cfg.Name != "f" {
		t.Errorf("Name = %q, want f", cfg.Name)
	}
}

func TestLoadParsesDurations(t *testing.T) {
	t.Setenv("TODO_SERVER__READ_TIMEOUT", "30s")

	var cfg testConfig
	if err := config.Load(&cfg, "TODO_", testDefaults(), ""); err != nil {
		t.Fatal(err)
	}
	if cfg.Server.ReadTimeout != 30*time.Second {
		t.Errorf("ReadTimeout = %v, want 30s", cfg.Server.ReadTimeout)
	}
}

func TestLoadWithoutEnvFile(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing.env")
	for _, envFile := range []string{missing, ""} {
		var cfg testConfig
		if err := config.Load(&cfg, "TODO_", testDefaults(), envFile); err != nil {
			t.Fatalf("envFile %q: %v", envFile, err)
		}
		if cfg.Server.Addr != "a" || cfg.Name != "d" {
			t.Errorf("envFile %q: got %+v, want defaults", envFile, cfg)
		}
	}
}

func TestLoadRefusesUnreadableEnvFile(t *testing.T) {
	var cfg testConfig
	if err := config.Load(&cfg, "TODO_", testDefaults(), t.TempDir()); err == nil {
		t.Fatal("expected an error for a directory used as env file")
	}
}

func TestLoadIgnoresOtherPrefixes(t *testing.T) {
	t.Setenv("OTHER_NAME", "x")

	var cfg testConfig
	if err := config.Load(&cfg, "TODO_", testDefaults(), ""); err != nil {
		t.Fatal(err)
	}
	if cfg.Name != "d" {
		t.Errorf("Name = %q, want d", cfg.Name)
	}
}

func TestProblemsReportsEveryProblem(t *testing.T) {
	p := config.NewProblems("TODO_")
	p.Require("crypt.keys", "")
	p.Require("server.addr", "set")
	p.Invalid("log.level", "must be debug, info, warn or error")

	err := p.Err()
	var ce *config.Error
	if !errors.As(err, &ce) {
		t.Fatalf("Err() = %v, want *config.Error", err)
	}
	if len(ce.Problems) != 2 {
		t.Fatalf("got %d problems, want 2", len(ce.Problems))
	}
	want := "config: TODO_CRYPT__KEYS is required; TODO_LOG__LEVEL must be debug, info, warn or error"
	if err.Error() != want {
		t.Errorf("message = %q, want %q", err.Error(), want)
	}
}

func TestProblemsNone(t *testing.T) {
	if err := config.NewProblems("TODO_").Err(); err != nil {
		t.Errorf("Err() = %v, want nil", err)
	}
}

func TestLoadHidesEnvFileContentsOnParseError(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path, []byte("TODO_CRYPT__KEYS=\"xchacha20poly1305:SECRET\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var cfg testConfig
	err := config.Load(&cfg, "TODO_", testDefaults(), path)
	if err == nil {
		t.Fatal("expected an error for an env file that does not parse")
	}
	if strings.Contains(err.Error(), "SECRET") {
		t.Errorf("error %q contains env file contents", err)
	}
}
