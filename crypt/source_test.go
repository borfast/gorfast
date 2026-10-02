package crypt_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"testing"

	"github.com/borfast/gorfast/crypt"
)

type sourceFunc func(context.Context) ([]crypt.Key, error)

func (f sourceFunc) Keys(ctx context.Context) ([]crypt.Key, error) { return f(ctx) }

func TestLoad(t *testing.T) {
	a, b := mustKey(t, crypt.XChaCha20Poly1305), mustKey(t, crypt.AES256GCM)
	kr, err := crypt.Load(t.Context(), crypt.StaticKeys(a.Spec(), b.Spec()))
	if err != nil {
		t.Fatal(err)
	}
	plaintext := []byte("message")
	current := sealForKeyring(t, kr, plaintext, bind)
	requireKeyringPlaintext(t, newKeyring(t, a), current, plaintext, bind)
	if kr.NeedsReseal(current) {
		t.Fatal("current value needs resealing")
	}
	retired := sealForKeyring(t, newKeyring(t, b), plaintext, bind)
	requireKeyringPlaintext(t, kr, retired, plaintext, bind)
	if !kr.NeedsReseal(retired) {
		t.Fatal("retired value does not need resealing")
	}
}

func TestLoadSplitsTheDocumentedWay(t *testing.T) {
	a, b := mustKey(t, crypt.XChaCha20Poly1305), mustKey(t, crypt.AES256GCM)
	env := a.Spec() + ", " + b.Spec() + "\n"
	if _, err := crypt.Load(t.Context(), crypt.StaticKeys(strings.Split(env, ",")...)); err != nil {
		t.Fatal(err)
	}
}

func TestLoadRefuses(t *testing.T) {
	b64 := func(n int) string { return base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{0x5c}, n)) }
	good := "xchacha20poly1305:" + b64(32)
	cases := []struct {
		name     string
		specs    []string
		position string
	}{
		{"no keys", nil, ""},
		{"trailing comma", strings.Split(good+",", ","), "key 2"},
		{"31 bytes", []string{good, "aes256gcm:" + b64(31)}, "key 2"},
		{"33 bytes", []string{"aes256gcm:" + b64(33)}, "key 1"},
		{"unknown algorithm", []string{"rot13:" + b64(32)}, "key 1"},
		{"no algorithm", []string{b64(32)}, "key 1"},
		{"invalid base64", []string{good, "aes256gcm:not-base64!"}, "key 2"},
		{"same key twice", []string{good, good}, "keys 1 and 2"},
		{"same material, two algorithms", []string{good, "aes256gcm:" + b64(32)}, "keys 1 and 2"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			kr, err := crypt.Load(t.Context(), crypt.StaticKeys(tc.specs...))
			if kr != nil || err == nil {
				t.Fatalf("Load = %v, %v; want nil, error", kr, err)
			}
			if !strings.Contains(err.Error(), tc.position) {
				t.Fatalf("error %q does not identify %q", err, tc.position)
			}
			for _, material := range []string{b64(31), b64(32), b64(33), "not-base64!"} {
				if strings.Contains(err.Error(), material) {
					t.Fatal("error contains key material")
				}
			}
		})
	}
}

func TestLoadSourceErrors(t *testing.T) {
	boom := errors.New("boom")
	kr, err := crypt.Load(t.Context(), sourceFunc(func(context.Context) ([]crypt.Key, error) {
		return nil, boom
	}))
	if kr != nil || !errors.Is(err, boom) || err == boom {
		t.Fatalf("Load = %v, %v; want nil, wrapped source error", kr, err)
	}
	kr, err = crypt.Load(t.Context(), sourceFunc(func(context.Context) ([]crypt.Key, error) {
		return nil, nil
	}))
	if kr != nil || err == nil {
		t.Fatalf("Load = %v, %v; want nil, error for empty source", kr, err)
	}
}

func TestLoadPassesContextToSource(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	kr, err := crypt.Load(ctx, sourceFunc(func(received context.Context) ([]crypt.Key, error) {
		return nil, received.Err()
	}))
	if kr != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("Load = %v, %v; want nil, context.Canceled", kr, err)
	}
}

func TestStaticKeysHonoursCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	for _, specs := range [][]string{{mustKey(t, crypt.AES256GCM).Spec()}, {"invalid"}, nil} {
		keys, err := crypt.StaticKeys(specs...).Keys(ctx)
		if keys != nil || !errors.Is(err, context.Canceled) {
			t.Fatalf("Keys = %v, %v; want nil, context.Canceled", keys, err)
		}
	}
}

func TestStaticKeysParsesEveryCall(t *testing.T) {
	a, b := mustKey(t, crypt.XChaCha20Poly1305), mustKey(t, crypt.AES256GCM)
	src := crypt.StaticKeys(a.Spec(), b.Spec())
	for i := 0; i < 2; i++ {
		keys, err := src.Keys(t.Context())
		if err != nil || len(keys) != 2 {
			t.Fatalf("Keys returned %d keys, %v; want two keys", len(keys), err)
		}
		if keys[0].String() != a.String() || keys[1].String() != b.String() {
			t.Fatal("Keys changed key identity or order")
		}
		keys[0] = crypt.Key{}
	}
}
