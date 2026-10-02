package crypt

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
)

// Algorithm identifies the authenticated cipher bound to a key.
type Algorithm uint8

const (
	// AES256GCM permits at most 2^32 seals per key across every instance.
	// Rotate the key well before reaching that limit.
	AES256GCM Algorithm = 0x01
	// XChaCha20Poly1305 uses a 192-bit nonce for authenticated encryption.
	XChaCha20Poly1305 Algorithm = 0x02
)

const keySize = 32
const keyIDLabel = "gorfast/crypt key id v1"

type keyData struct {
	alg Algorithm
	// The pointer prevents fmt from exposing bytes through unexported fields.
	material *[keySize]byte
	id       [8]byte
}

// Key binds secret material to one algorithm and prints only its key ID.
type Key struct{ d *keyData }

// ParseKey reads an algorithm name and a standard base64 encoded 32-byte key.
func ParseKey(spec string) (Key, error) {
	name, encoded, found := strings.Cut(strings.TrimSpace(spec), ":")
	if !found {
		return Key{}, errors.New("crypt: key spec requires algorithm and material")
	}
	var alg Algorithm
	for candidate, info := range algorithms {
		if info.name == name {
			alg = candidate
			break
		}
	}
	if _, ok := lookupAlgorithm(alg); !ok {
		return Key{}, errors.New("crypt: unknown key algorithm")
	}
	material, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return Key{}, errors.New("crypt: key material is not standard base64")
	}
	if len(material) != keySize {
		return Key{}, errors.New("crypt: key material must be 32 bytes")
	}
	var fixed [keySize]byte
	copy(fixed[:], material)
	return makeKey(alg, fixed), nil
}

// GenerateKey creates a random 32-byte key for the chosen algorithm.
func GenerateKey(alg Algorithm) (Key, error) {
	if _, ok := lookupAlgorithm(alg); !ok {
		return Key{}, errors.New("crypt: unknown key algorithm")
	}
	var material [keySize]byte
	if _, err := rand.Read(material[:]); err != nil {
		return Key{}, fmt.Errorf("crypt: generating key: %w", err)
	}
	return makeKey(alg, material), nil
}

func makeKey(alg Algorithm, material [keySize]byte) Key {
	d := &keyData{alg: alg, material: &material}
	mac := hmac.New(sha256.New, material[:])
	mac.Write([]byte(keyIDLabel))
	copy(d.id[:], mac.Sum(nil))
	return Key{d: d}
}

// String returns the algorithm and key ID without exposing secret material.
func (k Key) String() string {
	if k.d == nil {
		return "crypt.Key(invalid)"
	}
	alg, _ := lookupAlgorithm(k.d.alg)
	return alg.name + ":" + hex.EncodeToString(k.d.id[:])
}

// GoString returns the same safe representation as String.
func (k Key) GoString() string { return k.String() }

// Format writes the safe key representation for every formatting verb.
func (k Key) Format(f fmt.State, verb rune) { io.WriteString(f, k.String()) }

// Spec exports secret key material in configuration form; zero keys return "".
func (k Key) Spec() string {
	if k.d == nil {
		return ""
	}
	alg, _ := lookupAlgorithm(k.d.alg)
	return alg.name + ":" + base64.StdEncoding.EncodeToString(k.d.material[:])
}
