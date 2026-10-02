package crypt

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
	"testing"
	"testing/iotest"

	"golang.org/x/crypto/chacha20poly1305"
)

func TestFormatMatchesSpec(t *testing.T) {
	for _, tc := range []struct {
		name      string
		algByte   byte
		nonceSize int
	}{{"aes256gcm", 0x01, 12}, {"xchacha20poly1305", 0x02, 24}} {
		t.Run(tc.name, func(t *testing.T) {
			material := make([]byte, 32)
			for i := range material {
				material[i] = byte(i)
			}
			nonce := make([]byte, tc.nonceSize)
			for i := range nonce {
				nonce[i] = 0x40 + byte(i)
			}
			key, err := ParseKey(tc.name + ":" + base64.StdEncoding.EncodeToString(material))
			if err != nil {
				t.Fatal(err)
			}
			kr, err := NewKeyring(key)
			if err != nil {
				t.Fatal(err)
			}
			kr.rand = bytes.NewReader(nonce)
			binding := Bind("t", "c", "1")
			got, err := kr.Seal([]byte("gorfast"), binding)
			if err != nil {
				t.Fatal(err)
			}
			mac := hmac.New(sha256.New, material)
			mac.Write([]byte("gorfast/crypt key id v1"))
			header := append([]byte{0x01, tc.algByte}, mac.Sum(nil)[:8]...)
			var aead cipher.AEAD
			if tc.algByte == 0x01 {
				block, err := aes.NewCipher(material)
				if err != nil {
					t.Fatal(err)
				}
				aead, err = cipher.NewGCM(block)
				if err != nil {
					t.Fatal(err)
				}
			} else {
				aead, err = chacha20poly1305.NewX(material)
				if err != nil {
					t.Fatal(err)
				}
			}
			ad := append(bytes.Clone(header), binding...)
			want := append(bytes.Clone(header), nonce...)
			want = aead.Seal(want, nonce, []byte("gorfast"), ad)
			if !bytes.Equal(got.b, want) {
				t.Fatalf("wire format = %x; want %x", got.b, want)
			}
		})
	}
}

type nonceCountingReader struct{ n int }

func (r *nonceCountingReader) Read(p []byte) (int, error) {
	n, err := rand.Reader.Read(p)
	r.n += n
	return n, err
}

func TestNonceIsReadAtExactSize(t *testing.T) {
	for _, tc := range []struct {
		alg  Algorithm
		size int
	}{{AES256GCM, 12}, {XChaCha20Poly1305, 24}} {
		key, err := GenerateKey(tc.alg)
		if err != nil {
			t.Fatal(err)
		}
		kr, err := NewKeyring(key)
		if err != nil {
			t.Fatal(err)
		}
		r := &nonceCountingReader{}
		kr.rand = r
		if _, err := kr.Seal([]byte("x"), nil); err != nil {
			t.Fatal(err)
		}
		if r.n != tc.size {
			t.Fatalf("nonce read = %d; want %d", r.n, tc.size)
		}
	}
}

func TestSealFailsWithoutRandomness(t *testing.T) {
	noEntropy := errors.New("no entropy")
	for _, alg := range []Algorithm{AES256GCM, XChaCha20Poly1305} {
		key, err := GenerateKey(alg)
		if err != nil {
			t.Fatal(err)
		}
		kr, err := NewKeyring(key)
		if err != nil {
			t.Fatal(err)
		}
		for _, tc := range []struct {
			reader io.Reader
			want   error
		}{
			{iotest.ErrReader(noEntropy), noEntropy}, {bytes.NewReader(make([]byte, 5)), io.ErrUnexpectedEOF},
		} {
			kr.rand = tc.reader
			s, err := kr.Seal([]byte("x"), nil)
			if !errors.Is(err, tc.want) || !s.IsZero() {
				t.Fatalf("Seal without entropy = zero %v, %v; want %v", s.IsZero(), err, tc.want)
			}
		}
	}
}

func TestAlgorithmMismatchRejectedBeforeDecryption(t *testing.T) {
	material := bytes.Repeat([]byte{0x67}, 32)
	key, err := ParseKey("xchacha20poly1305:" + base64.StdEncoding.EncodeToString(material))
	if err != nil {
		t.Fatal(err)
	}
	kr, err := NewKeyring(key)
	if err != nil {
		t.Fatal(err)
	}
	block, err := aes.NewCipher(material)
	if err != nil {
		t.Fatal(err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatal(err)
	}
	header := append([]byte{0x01, 0x01}, key.d.id[:]...)
	nonce := make([]byte, 12)
	binding := Bind("messages", "body", "42")
	ad := append(bytes.Clone(header), binding...)
	sealed := append(bytes.Clone(header), nonce...)
	sealed = aead.Seal(sealed, nonce, []byte("valid AES"), ad)
	pt, err := aead.Open(nil, nonce, sealed[22:], ad)
	if err != nil || string(pt) != "valid AES" {
		t.Fatal("AES fixture is not valid")
	}
	pt, err = kr.Open(Sealed{b: sealed}, binding)
	if !errors.Is(err, ErrCannotOpen) || pt != nil {
		t.Fatalf("algorithm mismatch: Open = %q, %v", pt, err)
	}
}
