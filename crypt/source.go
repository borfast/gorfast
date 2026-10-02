package crypt

import (
	"context"
	"errors"
	"fmt"
	"io"
)

// KeySource supplies keys in order, with the current key first.
// Implementations must pass crypttest.RunKeySource.
type KeySource interface {
	Keys(ctx context.Context) ([]Key, error)
}

type staticKeys struct {
	// Two pointers stop fmt's fallback from dereferencing secret specs.
	specs **[]string
}

// StaticKeys supplies keys by parsing the specs on every Keys call.
func StaticKeys(specs ...string) KeySource {
	copied := append([]string(nil), specs...)
	stored := &copied
	return staticKeys{specs: &stored}
}

func (s staticKeys) Format(f fmt.State, verb rune) {
	io.WriteString(f, "crypt.StaticKeys(redacted)")
}

func (s staticKeys) Keys(ctx context.Context) ([]Key, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	specs := **s.specs
	keys := make([]Key, len(specs))
	for i, spec := range specs {
		key, err := ParseKey(spec)
		if err != nil {
			return nil, fmt.Errorf("crypt: key %d: %w", i+1, err)
		}
		keys[i] = key
	}
	return keys, nil
}

// Load creates a keyring from a source, using its first key to seal values.
func Load(ctx context.Context, src KeySource) (*Keyring, error) {
	keys, err := src.Keys(ctx)
	if err != nil {
		return nil, fmt.Errorf("crypt: loading keys: %w", err)
	}
	if len(keys) == 0 {
		return nil, errors.New("crypt: no keys")
	}
	return NewKeyring(keys[0], keys[1:]...)
}
