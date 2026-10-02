package crypt_test

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"testing"

	"github.com/borfast/gorfast/crypt"
)

func FuzzOpen(f *testing.F) {
	data, err := os.ReadFile("testdata/vectors.json")
	if err != nil {
		f.Fatal(err)
	}
	var vectors []struct {
		Key     string `json:"key"`
		Binding string `json:"binding"`
		Sealed  string `json:"sealed"`
	}
	if err := json.Unmarshal(data, &vectors); err != nil {
		f.Fatal(err)
	}
	if len(vectors) != 4 {
		f.Fatalf("got %d vectors, want 4", len(vectors))
	}
	decode := func(value string) []byte {
		f.Helper()
		decoded, err := hex.DecodeString(value)
		if err != nil {
			f.Fatal(err)
		}
		return decoded
	}
	var keys []crypt.Key
	seenKeys := map[string]bool{}
	for _, vector := range vectors {
		if !seenKeys[vector.Key] {
			key, err := crypt.ParseKey(vector.Key)
			if err != nil {
				f.Fatal(err)
			}
			keys = append(keys, key)
			seenKeys[vector.Key] = true
		}
		f.Add(decode(vector.Sealed), decode(vector.Binding))
	}
	if len(keys) != 2 {
		f.Fatalf("got %d vector keys, want 2", len(keys))
	}
	kr, err := crypt.NewKeyring(keys[0], keys[1])
	if err != nil {
		f.Fatal(err)
	}
	f.Add([]byte(nil), []byte(nil))
	f.Add([]byte{0x01}, []byte(nil))
	sealed := decode(vectors[0].Sealed)
	if len(sealed) < 10 {
		f.Fatal("vector is shorter than its header")
	}
	f.Add(sealed[:10], []byte(nil))
	f.Fuzz(func(t *testing.T, data, binding []byte) {
		s := crypt.FromCiphertext(data)
		_ = kr.NeedsReseal(s)
		_, err := kr.Open(s, binding)
		if err != nil && !errors.Is(err, crypt.ErrCannotOpen) && !errors.Is(err, crypt.ErrUnknownKey) {
			t.Fatalf("non-sentinel error: %v", err)
		}
	})
}
