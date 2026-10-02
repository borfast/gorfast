// Package crypt supports encryption at rest with explicit keys and bindings
// that associate ciphertext with its storage location.
package crypt

import (
	"bytes"
	"encoding/binary"
	"errors"
	"math"
)

// Encryptor seals and opens values with a binding. Implementations must be
// safe for concurrent use and pass crypttest.RunEncryptor.
type Encryptor interface {
	Seal(plaintext, binding []byte) (Sealed, error)
	Open(s Sealed, binding []byte) ([]byte, error)
}

// Sealed owns ciphertext bytes; its zero value represents an absent value.
type Sealed struct{ b []byte }

// IsZero reports whether the value is absent, rather than merely empty.
func (s Sealed) IsZero() bool { return s.b == nil }

// Ciphertext returns a copy of the bytes, or nil for the zero value.
func (s Sealed) Ciphertext() []byte { return bytes.Clone(s.b) }

// FromCiphertext copies bytes into a Sealed, preserving nil and empty slices.
func FromCiphertext(b []byte) Sealed { return Sealed{b: bytes.Clone(b)} }

// Bind encodes each part with a four-byte big-endian length prefix.
// It panics if any part exceeds math.MaxUint32 bytes.
func Bind(parts ...string) []byte {
	var binding []byte
	for _, part := range parts {
		if uint64(len(part)) > math.MaxUint32 {
			panic("crypt: binding part exceeds math.MaxUint32 bytes")
		}
		binding = binary.BigEndian.AppendUint32(binding, uint32(len(part)))
		binding = append(binding, part...)
	}
	return binding
}

var (
	// ErrUnknownKey means the value names a key missing from the keyring.
	ErrUnknownKey = errors.New("crypt: sealed under a key not in the keyring")
	// ErrCannotOpen means a sealed value could not be authenticated and opened.
	ErrCannotOpen = errors.New("crypt: cannot open sealed value")
)
