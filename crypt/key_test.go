package crypt_test

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"

	"github.com/borfast/gorfast/crypt"
)

func TestGenerateKeyRoundTripsThroughSpec(t *testing.T) {
	prefixes := map[crypt.Algorithm]string{
		crypt.XChaCha20Poly1305: "xchacha20poly1305:",
		crypt.AES256GCM:         "aes256gcm:",
	}
	for alg, prefix := range prefixes {
		t.Run(prefix, func(t *testing.T) {
			k, err := crypt.GenerateKey(alg)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.HasPrefix(k.Spec(), prefix) {
				t.Fatalf("wrong algorithm prefix: %s", k)
			}
			raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(k.Spec(), prefix))
			if err != nil || len(raw) != 32 {
				t.Fatal("spec must encode 32 bytes")
			}
			parsed, err := crypt.ParseKey(k.Spec())
			if err != nil {
				t.Fatal(err)
			}
			if parsed.String() != k.String() || parsed.Spec() != k.Spec() {
				t.Fatal("key round trip changed key")
			}
		})
	}
	first, err := crypt.GenerateKey(crypt.XChaCha20Poly1305)
	if err != nil {
		t.Fatal(err)
	}
	second, err := crypt.GenerateKey(crypt.XChaCha20Poly1305)
	if err != nil {
		t.Fatal(err)
	}
	if first.String() == second.String() {
		t.Fatal("generated duplicate keys")
	}
	if _, err := crypt.GenerateKey(crypt.Algorithm(0x07)); err == nil {
		t.Fatal("unsupported algorithm accepted")
	}
}

func TestKeyID(t *testing.T) {
	material := bytes.Repeat([]byte{0x2a}, 32)
	k, err := crypt.ParseKey("aes256gcm:" + base64.StdEncoding.EncodeToString(material))
	if err != nil {
		t.Fatal(err)
	}
	mac := hmac.New(sha256.New, material)
	mac.Write([]byte("gorfast/crypt key id v1"))
	want := "aes256gcm:" + hex.EncodeToString(mac.Sum(nil)[:8])
	if k.String() != want {
		t.Fatalf("ID = %s, want %s", k, want)
	}
}

func TestParseKeyTrimsWhitespace(t *testing.T) {
	k, err := crypt.GenerateKey(crypt.AES256GCM)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := crypt.ParseKey(" " + k.Spec() + "\n")
	if err != nil {
		t.Fatal(err)
	}
	if parsed.String() != k.String() || parsed.Spec() != k.Spec() {
		t.Fatal("whitespace changed key")
	}
}

func TestParseKeyRefuses(t *testing.T) {
	b64 := func(n int) string { return base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{0x5c}, n)) }
	cases := map[string]string{
		"empty": "", "no algorithm": b64(32), "unknown name": "rot13:" + b64(32),
		"wrong case": "AES256GCM:" + b64(32), "31 bytes": "aes256gcm:" + b64(31),
		"33 bytes": "aes256gcm:" + b64(33), "not base64": "aes256gcm:" + strings.Repeat("!", 44),
		"material first": b64(32) + ":aes256gcm",
	}
	for name, spec := range cases {
		t.Run(name, func(t *testing.T) {
			k, err := crypt.ParseKey(spec)
			if err == nil {
				t.Fatal("invalid spec accepted")
			}
			if k.Spec() != "" {
				t.Fatal("failed parse returned key material")
			}
			for _, material := range []string{b64(31), b64(32), b64(33), strings.Repeat("!", 44)} {
				if strings.Contains(err.Error(), material) {
					t.Fatal("error leaked material")
				}
			}
		})
	}
}

func TestKeyNeverPrintsMaterial(t *testing.T) {
	k, err := crypt.GenerateKey(crypt.XChaCha20Poly1305)
	if err != nil {
		t.Fatal(err)
	}
	b64 := strings.SplitN(k.Spec(), ":", 2)[1]
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		t.Fatal(err)
	}
	forbidden := []string{b64, hex.EncodeToString(raw), strings.ToUpper(hex.EncodeToString(raw)), fmt.Sprint(raw), string(raw)}
	type config struct {
		key  crypt.Key
		keys []crypt.Key
	}
	values := []any{k, &k, []crypt.Key{k}, config{k, []crypt.Key{k}}, crypt.Key{}}
	for _, v := range values {
		for _, verb := range []string{"%v", "%+v", "%#v", "%s", "%q", "%x", "%X", "%d"} {
			out := fmt.Sprintf(verb, v)
			for _, material := range forbidden {
				if strings.Contains(out, material) {
					t.Fatalf("%s leaked material", verb)
				}
			}
		}
	}
	for _, verb := range []string{"%v", "%+v", "%#v", "%s", "%q", "%x", "%X", "%d"} {
		if fmt.Sprintf(verb, k) != k.String() {
			t.Fatalf("%s did not print key ID", verb)
		}
	}
	if k.GoString() != k.String() {
		t.Fatal("GoString did not print key ID")
	}
}

func TestZeroKey(t *testing.T) {
	var k crypt.Key
	if k.String() != "crypt.Key(invalid)" || k.GoString() != "crypt.Key(invalid)" {
		t.Fatal("wrong zero key display")
	}
	if k.Spec() != "" {
		t.Fatal("zero key exported material")
	}
	for _, verb := range []string{"%v", "%+v", "%#v", "%s", "%q", "%x", "%X", "%d"} {
		if fmt.Sprintf(verb, k) != "crypt.Key(invalid)" {
			t.Fatalf("%s printed wrong zero key", verb)
		}
	}
}
