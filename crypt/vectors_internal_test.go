package crypt

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

type frozenVector struct {
	Name      string `json:"name"`
	Key       string `json:"key"`
	Plaintext string `json:"plaintext"`
	Binding   string `json:"binding"`
	Sealed    string `json:"sealed"`
}

func TestVectorsKeepOpening(t *testing.T) {
	path := filepath.Join("testdata", "vectors.json")
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		writeVectors(t, path)
		t.Fatalf("wrote %s: review it, commit it, and run the tests again", path)
	}
	if err != nil {
		t.Fatal(err)
	}
	var vectors []frozenVector
	if err := json.Unmarshal(data, &vectors); err != nil {
		t.Fatal(err)
	}
	if len(vectors) != 4 {
		t.Fatalf("got %d vectors, want 4", len(vectors))
	}
	wantLengths := map[string]int{
		"xchacha20poly1305 text":  66,
		"xchacha20poly1305 empty": 50,
		"aes256gcm text":          54,
		"aes256gcm empty":         38,
	}
	seenAlgorithms := map[byte]bool{}
	for _, vector := range vectors {
		t.Run(vector.Name, func(t *testing.T) {
			wantLength, ok := wantLengths[vector.Name]
			if !ok {
				t.Fatalf("unexpected or duplicate vector %q", vector.Name)
			}
			delete(wantLengths, vector.Name)
			decode := func(value string) []byte {
				t.Helper()
				decoded, err := hex.DecodeString(value)
				if err != nil {
					t.Fatal(err)
				}
				return decoded
			}
			sealed := decode(vector.Sealed)
			if len(sealed) != wantLength {
				t.Fatalf("sealed length = %d, want %d", len(sealed), wantLength)
			}
			if sealed[0] != 0x01 || (sealed[1] != 0x01 && sealed[1] != 0x02) {
				t.Fatalf("unexpected format header: %x", sealed[:2])
			}
			seenAlgorithms[sealed[1]] = true
			key, err := ParseKey(vector.Key)
			if err != nil {
				t.Fatal(err)
			}
			kr, err := NewKeyring(key)
			if err != nil {
				t.Fatal(err)
			}
			plaintext, err := kr.Open(FromCiphertext(sealed), decode(vector.Binding))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(plaintext, decode(vector.Plaintext)) {
				t.Fatalf("plaintext = %x, want %s", plaintext, vector.Plaintext)
			}
		})
	}
	if !seenAlgorithms[0x01] || !seenAlgorithms[0x02] {
		t.Fatal("vectors must cover both algorithm bytes")
	}
}

func writeVectors(t *testing.T, path string) {
	t.Helper()
	var vectors []frozenVector
	for _, algorithm := range []struct {
		name      string
		keyStart  byte
		nonceSize int
	}{
		{"xchacha20poly1305", 0x00, 24},
		{"aes256gcm", 0x20, 12},
	} {
		material := make([]byte, 32)
		for i := range material {
			material[i] = algorithm.keyStart + byte(i)
		}
		spec := algorithm.name + ":" + base64.StdEncoding.EncodeToString(material)
		key, err := ParseKey(spec)
		if err != nil {
			t.Fatal(err)
		}
		kr, err := NewKeyring(key)
		if err != nil {
			t.Fatal(err)
		}
		nonce := make([]byte, algorithm.nonceSize)
		for i := range nonce {
			nonce[i] = 0x40 + byte(i)
		}
		for _, input := range []struct {
			name               string
			plaintext, binding []byte
		}{
			{"text", []byte("gorfast crypt v1"), Bind("messages", "body", "42")},
			{"empty", nil, nil},
		} {
			kr.rand = bytes.NewReader(nonce)
			sealed, err := kr.Seal(input.plaintext, input.binding)
			if err != nil {
				t.Fatal(err)
			}
			vectors = append(vectors, frozenVector{
				Name:      algorithm.name + " " + input.name,
				Key:       spec,
				Plaintext: hex.EncodeToString(input.plaintext),
				Binding:   hex.EncodeToString(input.binding),
				Sealed:    hex.EncodeToString(sealed.Ciphertext()),
			})
		}
	}
	data, err := json.MarshalIndent(vectors, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		t.Fatal(err)
	}
	_, writeErr := file.Write(append(data, '\n'))
	closeErr := file.Close()
	if writeErr != nil {
		t.Fatal(writeErr)
	}
	if closeErr != nil {
		t.Fatal(closeErr)
	}
}
