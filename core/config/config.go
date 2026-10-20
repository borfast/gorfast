// Package config loads layered configuration with Koanf: defaults, then an
// optional .env file, then environment variables.
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"strings"

	"github.com/knadh/koanf/parsers/dotenv"
	"github.com/knadh/koanf/providers/confmap"
	"github.com/knadh/koanf/providers/env/v2"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
)

// Load fills target, a pointer to a struct with koanf tags. Later layers win:
// defaults, then envFile if it exists ("" skips it), then environment variables.
func Load(target any, prefix string, defaults map[string]any, envFile string) error {
	k := koanf.New(".")

	if err := k.Load(confmap.Provider(defaults, "."), nil); err != nil {
		return fmt.Errorf("loading defaults: %w", err)
	}

	if envFile != "" {
		transform := func(name string) string { return TransformKey(prefix, name) }
		raw, err := file.Provider(envFile).ReadBytes()
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("loading env file: %w", err)
		}
		if err == nil {
			// The parser's error quotes the file, which may hold secrets, so drop it.
			values, perr := dotenv.ParserEnv(prefix, ".", transform).Unmarshal(raw)
			if perr != nil {
				return fmt.Errorf("config: env file %s does not parse", envFile)
			}
			if err := k.Load(confmap.Provider(values, "."), nil); err != nil {
				return fmt.Errorf("loading env file: %w", err)
			}
		}
	}

	err := k.Load(env.Provider(".", env.Opt{
		Prefix: prefix,
		TransformFunc: func(name, value string) (string, any) {
			return TransformKey(prefix, name), value
		},
	}), nil)
	if err != nil {
		return fmt.Errorf("loading environment variables: %w", err)
	}

	if err := k.Unmarshal("", target); err != nil {
		return unmarshalError(prefix, err)
	}
	return nil
}

// unmarshalError names each setting that did not decode. It drops the decoder's
// message, which can quote the value, and a value may be a secret.
func unmarshalError(prefix string, err error) error {
	p := NewProblems(prefix)
	for _, key := range failedKeys(err) {
		p.Invalid(key, "has a value that does not parse")
	}
	if err := p.Err(); err != nil {
		return err
	}
	return errors.New("config: the settings do not fit target, which must be a pointer to a struct")
}

// failedKeys returns the deepest key each decoder error names. Matching the Name
// method of mapstructure's DecodeError avoids importing mapstructure.
func failedKeys(err error) []string {
	var keys []string
	seen := map[string]bool{}
	var walk func(err error) bool
	walk = func(err error) bool {
		found := false
		switch u := err.(type) {
		case interface{ Unwrap() []error }:
			for _, e := range u.Unwrap() {
				found = walk(e) || found
			}
		case interface{ Unwrap() error }:
			if e := u.Unwrap(); e != nil {
				found = walk(e)
			}
		}
		if n, ok := err.(interface{ Name() string }); ok && !found && n.Name() != "" {
			if !seen[n.Name()] {
				seen[n.Name()] = true
				keys = append(keys, n.Name())
			}
			return true
		}
		return found
	}
	walk(err)
	return keys
}

// TransformKey turns TODO_DATABASE__MAX_CONNS with prefix TODO_ into database.max_conns.
func TransformKey(prefix, name string) string {
	s := strings.ToLower(strings.TrimPrefix(name, prefix))
	return strings.ReplaceAll(s, "__", ".")
}

// Variable is the inverse of TransformKey: database.max_conns becomes TODO_DATABASE__MAX_CONNS.
func Variable(prefix, key string) string {
	return prefix + strings.ToUpper(strings.ReplaceAll(key, ".", "__"))
}

// Problem is one configuration setting that is missing or wrong.
type Problem struct {
	Key      string
	Variable string
	Reason   string
}

// Error lists every problem found, so one run shows them all.
type Error struct {
	Problems []Problem
}

func (e *Error) Error() string {
	parts := make([]string, len(e.Problems))
	for i, p := range e.Problems {
		parts[i] = p.Variable + " " + p.Reason
	}
	return "config: " + strings.Join(parts, "; ")
}

// Problems collects configuration problems for one prefix.
type Problems struct {
	prefix string
	list   []Problem
}

// NewProblems returns an empty collection for variables starting with prefix.
func NewProblems(prefix string) *Problems {
	return &Problems{prefix: prefix}
}

// Require records a problem when value is empty.
func (p *Problems) Require(key, value string) {
	if value == "" {
		p.Invalid(key, "is required")
	}
}

// Invalid records a problem for key with the given reason, used as written.
func (p *Problems) Invalid(key, reason string) {
	p.list = append(p.list, Problem{Key: key, Variable: Variable(p.prefix, key), Reason: reason})
}

// Err returns a *Error with every problem recorded, or nil if there were none.
func (p *Problems) Err() error {
	if len(p.list) == 0 {
		return nil
	}
	return &Error{Problems: p.list}
}
