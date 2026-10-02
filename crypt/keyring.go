package crypt

import (
	"crypto/cipher"
	"crypto/rand"
	"fmt"
	"io"
)

const headerSize = 10
const formatVersion = 0x01

type keyringKey struct {
	alg  Algorithm
	id   [8]byte
	aead cipher.AEAD
}

// Keyring seals with its current key and opens with current or retired keys.
// It is safe for concurrent use.
type Keyring struct {
	rand    io.Reader
	current *keyringKey
	keys    map[[8]byte]*keyringKey
}

// NewKeyring creates a keyring, rejecting zero keys and duplicate key IDs.
func NewKeyring(current Key, retired ...Key) (*Keyring, error) {
	keys := make([]Key, 1, 1+len(retired))
	keys[0] = current
	keys = append(keys, retired...)
	kr := &Keyring{rand: rand.Reader, keys: make(map[[8]byte]*keyringKey, len(keys))}
	positions := make(map[[8]byte]int, len(keys))
	for i, key := range keys {
		if key.d == nil {
			return nil, fmt.Errorf("crypt: key %d is zero", i+1)
		}
		if previous, exists := positions[key.d.id]; exists {
			return nil, fmt.Errorf("crypt: keys %d and %d have duplicate key ID %x", previous, i+1, key.d.id)
		}
		alg, ok := lookupAlgorithm(key.d.alg)
		if !ok {
			return nil, fmt.Errorf("crypt: key %d has an unknown algorithm", i+1)
		}
		aead, err := alg.newAEAD(key.d.material[:])
		if err != nil {
			return nil, fmt.Errorf("crypt: initializing key %d: %w", i+1, err)
		}
		entry := &keyringKey{alg: key.d.alg, id: key.d.id, aead: aead}
		kr.keys[entry.id] = entry
		positions[entry.id] = i + 1
		if i == 0 {
			kr.current = entry
		}
	}
	return kr, nil
}

// Seal encrypts plaintext with the current key and authenticates its binding.
func (k *Keyring) Seal(plaintext, binding []byte) (Sealed, error) {
	key := k.current
	nonceSize := key.aead.NonceSize()
	envelope := make([]byte, headerSize+nonceSize, headerSize+nonceSize+len(plaintext)+key.aead.Overhead())
	envelope[0] = formatVersion
	envelope[1] = byte(key.alg)
	copy(envelope[2:headerSize], key.id[:])
	nonce := envelope[headerSize:]
	if _, err := io.ReadFull(k.rand, nonce); err != nil {
		return Sealed{}, fmt.Errorf("crypt: reading nonce: %w", err)
	}
	ad := authenticatedData(envelope[:headerSize], binding)
	envelope = key.aead.Seal(envelope, nonce, plaintext, ad)
	return Sealed{b: envelope}, nil
}

// Open authenticates a sealed value and its binding before returning plaintext.
func (k *Keyring) Open(s Sealed, binding []byte) ([]byte, error) {
	plaintext, _, err := k.open(s, binding)
	return plaintext, err
}

func (k *Keyring) open(s Sealed, binding []byte) ([]byte, *keyringKey, error) {
	if len(s.b) < headerSize || s.b[0] != formatVersion {
		return nil, nil, ErrCannotOpen
	}
	alg := Algorithm(s.b[1])
	if _, ok := lookupAlgorithm(alg); !ok {
		return nil, nil, ErrCannotOpen
	}
	var id [8]byte
	copy(id[:], s.b[2:headerSize])
	key, ok := k.keys[id]
	if !ok {
		return nil, nil, fmt.Errorf("%w (key ID %x)", ErrUnknownKey, id)
	}
	if alg != key.alg {
		return nil, nil, ErrCannotOpen
	}
	nonceEnd := headerSize + key.aead.NonceSize()
	if len(s.b) < nonceEnd+key.aead.Overhead() {
		return nil, nil, ErrCannotOpen
	}
	plaintext, err := key.aead.Open(nil, s.b[headerSize:nonceEnd], s.b[nonceEnd:], authenticatedData(s.b[:headerSize], binding))
	if err != nil {
		return nil, nil, ErrCannotOpen
	}
	return plaintext, key, nil
}

// NeedsReseal reads the header as a hint; it does not authenticate the value.
func (k *Keyring) NeedsReseal(s Sealed) bool {
	if len(s.b) < headerSize || s.b[0] != formatVersion {
		return false
	}
	alg := Algorithm(s.b[1])
	if _, ok := lookupAlgorithm(alg); !ok {
		return false
	}
	var id [8]byte
	copy(id[:], s.b[2:headerSize])
	key, ok := k.keys[id]
	return ok && key != k.current && alg == key.alg
}

// Reseal authenticates first, then replaces values opened under a retired key.
func (k *Keyring) Reseal(s Sealed, binding []byte) (Sealed, bool, error) {
	plaintext, key, err := k.open(s, binding)
	if err != nil {
		return Sealed{}, false, err
	}
	if key == k.current {
		return s, false, nil
	}
	resealed, err := k.Seal(plaintext, binding)
	if err != nil {
		return Sealed{}, false, err
	}
	return resealed, true, nil
}

func authenticatedData(header, binding []byte) []byte {
	ad := make([]byte, 0, len(header)+len(binding))
	ad = append(ad, header...)
	return append(ad, binding...)
}
