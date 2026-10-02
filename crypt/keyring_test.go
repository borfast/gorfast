package crypt_test

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/borfast/gorfast/crypt"
)

var bind = crypt.Bind("messages", "body", "42")

func mustKey(t *testing.T, alg crypt.Algorithm) crypt.Key {
	t.Helper()
	k, err := crypt.GenerateKey(alg)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func newKeyring(t *testing.T, current crypt.Key, retired ...crypt.Key) *crypt.Keyring {
	t.Helper()
	kr, err := crypt.NewKeyring(current, retired...)
	if err != nil {
		t.Fatal(err)
	}
	return kr
}

func requireCannotOpen(t *testing.T, pt []byte, err error) {
	t.Helper()
	if !errors.Is(err, crypt.ErrCannotOpen) || pt != nil {
		t.Fatalf("Open = %q, %v; want nil, ErrCannotOpen", pt, err)
	}
}

func sealForKeyring(t *testing.T, kr *crypt.Keyring, plaintext, binding []byte) crypt.Sealed {
	t.Helper()
	s, err := kr.Seal(plaintext, binding)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func requireKeyringPlaintext(t *testing.T, kr *crypt.Keyring, s crypt.Sealed, plaintext, binding []byte) {
	t.Helper()
	pt, err := kr.Open(s, binding)
	if err != nil || !bytes.Equal(pt, plaintext) {
		t.Fatalf("Open = %q, %v; want %q, nil", pt, err, plaintext)
	}
}

func TestNewKeyringRefuses(t *testing.T) {
	a := mustKey(t, crypt.XChaCha20Poly1305)
	sameMaterial, err := crypt.ParseKey("aes256gcm:" + strings.SplitN(a.Spec(), ":", 2)[1])
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name      string
		keys      []crypt.Key
		positions string
	}{
		{"zero current", []crypt.Key{{}}, ""},
		{"zero retired", []crypt.Key{a, {}}, ""},
		{"duplicate", []crypt.Key{a, a}, "keys 1 and 2"},
		{"same material other algorithm", []crypt.Key{a, sameMaterial}, "keys 1 and 2"},
		{"duplicate retired", []crypt.Key{mustKey(t, crypt.AES256GCM), a, a}, "keys 2 and 3"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			kr, err := crypt.NewKeyring(tc.keys[0], tc.keys[1:]...)
			if err == nil || kr != nil {
				t.Fatalf("NewKeyring = %v, %v; want nil and error", kr, err)
			}
			if !strings.Contains(err.Error(), tc.positions) {
				t.Fatalf("error %q does not identify %q", err, tc.positions)
			}
		})
	}
}

func TestNilAndEmptyBindingAreTheSame(t *testing.T) {
	for _, alg := range []crypt.Algorithm{crypt.AES256GCM, crypt.XChaCha20Poly1305} {
		kr := newKeyring(t, mustKey(t, alg))
		s := sealForKeyring(t, kr, []byte("x"), nil)
		requireKeyringPlaintext(t, kr, s, []byte("x"), []byte{})
		requireKeyringPlaintext(t, kr, s, []byte("x"), crypt.Bind())
		s = sealForKeyring(t, kr, []byte("x"), []byte{})
		requireKeyringPlaintext(t, kr, s, []byte("x"), nil)
	}
}

func TestOverhead(t *testing.T) {
	for _, tc := range []struct {
		alg      crypt.Algorithm
		overhead int
	}{{crypt.AES256GCM, 38}, {crypt.XChaCha20Poly1305, 50}} {
		kr := newKeyring(t, mustKey(t, tc.alg))
		for _, size := range []int{0, 100} {
			pt := bytes.Repeat([]byte("p"), size)
			s := sealForKeyring(t, kr, pt, bind)
			if s.IsZero() || len(s.Ciphertext())-size != tc.overhead {
				t.Fatalf("algorithm %d, size %d: ciphertext length %d, zero %v", tc.alg, size, len(s.Ciphertext()), s.IsZero())
			}
			requireKeyringPlaintext(t, kr, s, pt, bind)
		}
	}
}

func TestHeaderHandling(t *testing.T) {
	for _, alg := range []crypt.Algorithm{crypt.AES256GCM, crypt.XChaCha20Poly1305} {
		kr := newKeyring(t, mustKey(t, alg))
		s := sealForKeyring(t, kr, []byte("x"), bind)
		for _, tc := range []struct {
			offset int
			value  byte
		}{{0, 0x02}, {1, 0x03}, {1, 3 - byte(alg)}} {
			c := s.Ciphertext()
			c[tc.offset] = tc.value
			pt, err := kr.Open(crypt.FromCiphertext(c), bind)
			requireCannotOpen(t, pt, err)
		}
		c := s.Ciphertext()
		c[2] ^= 0xff
		pt, err := kr.Open(crypt.FromCiphertext(c), bind)
		if !errors.Is(err, crypt.ErrUnknownKey) || pt != nil {
			t.Fatalf("unknown key: Open = %q, %v", pt, err)
		}
		if !strings.Contains(err.Error(), hex.EncodeToString(c[2:10])) {
			t.Fatalf("unknown key error does not name header key ID: %v", err)
		}
		for size := 0; size < len(s.Ciphertext()); size++ {
			pt, err := kr.Open(crypt.FromCiphertext(s.Ciphertext()[:size]), bind)
			requireCannotOpen(t, pt, err)
		}
	}
}

func TestRotation(t *testing.T) {
	a, b := mustKey(t, crypt.XChaCha20Poly1305), mustKey(t, crypt.AES256GCM)
	phase0, phase1 := newKeyring(t, a), newKeyring(t, a, b)
	phase2, after := newKeyring(t, b, a), newKeyring(t, b)
	pt := []byte("rotation")
	requireKeyringPlaintext(t, phase0, sealForKeyring(t, phase1, pt, bind), pt, bind)
	requireKeyringPlaintext(t, phase1, sealForKeyring(t, phase2, pt, bind), pt, bind)
	old := sealForKeyring(t, phase0, pt, bind)
	requireKeyringPlaintext(t, phase2, old, pt, bind)
	if !phase2.NeedsReseal(old) {
		t.Fatal("retired value does not need resealing")
	}
	r, changed, err := phase2.Reseal(old, bind)
	if err != nil || !changed || bytes.Equal(r.Ciphertext(), old.Ciphertext()) {
		t.Fatalf("Reseal retired = %v, %v; ciphertext equal %v", changed, err, bytes.Equal(r.Ciphertext(), old.Ciphertext()))
	}
	requireKeyringPlaintext(t, phase2, r, pt, bind)
	if phase2.NeedsReseal(r) {
		t.Fatal("current value needs resealing")
	}
	unchanged, changed, err := phase2.Reseal(r, bind)
	if err != nil || changed || !bytes.Equal(unchanged.Ciphertext(), r.Ciphertext()) {
		t.Fatalf("Reseal current = %v, %v; want unchanged bytes", changed, err)
	}
	got, err := after.Open(old, bind)
	if !errors.Is(err, crypt.ErrUnknownKey) || got != nil {
		t.Fatalf("removed key: Open = %q, %v", got, err)
	}
	requireKeyringPlaintext(t, after, r, pt, bind)
}

func TestResealAuthenticates(t *testing.T) {
	kr := newKeyring(t, mustKey(t, crypt.XChaCha20Poly1305))
	s := sealForKeyring(t, kr, []byte("x"), bind)
	c := s.Ciphertext()
	c[len(c)-1] ^= 1
	for _, tc := range []struct {
		s       crypt.Sealed
		binding []byte
	}{
		{crypt.FromCiphertext(c), bind}, {s, crypt.Bind("other")}, {crypt.Sealed{}, bind},
	} {
		r, changed, err := kr.Reseal(tc.s, tc.binding)
		if !errors.Is(err, crypt.ErrCannotOpen) || changed || !r.IsZero() {
			t.Fatalf("Reseal invalid = zero %v, changed %v, %v", r.IsZero(), changed, err)
		}
	}
}

func TestNeedsResealIsOnlyAHint(t *testing.T) {
	a, b, c := mustKey(t, crypt.XChaCha20Poly1305), mustKey(t, crypt.AES256GCM), mustKey(t, crypt.XChaCha20Poly1305)
	old := sealForKeyring(t, newKeyring(t, a), []byte("x"), bind)
	kr := newKeyring(t, b, a)
	cur := sealForKeyring(t, kr, []byte("x"), bind)
	badVersion := old.Ciphertext()
	badVersion[0] = 0x02
	badAlgorithm := old.Ciphertext()
	badAlgorithm[1] = 0x03
	mismatch := old.Ciphertext()
	mismatch[1] = byte(crypt.AES256GCM)
	for _, s := range []crypt.Sealed{crypt.Sealed{}, crypt.FromCiphertext([]byte{1}), crypt.FromCiphertext(badVersion), crypt.FromCiphertext(badAlgorithm), crypt.FromCiphertext(mismatch), sealForKeyring(t, newKeyring(t, c), []byte("x"), bind), cur} {
		if kr.NeedsReseal(s) {
			t.Fatal("invalid, unknown, or current header needs resealing")
		}
	}
	for _, tc := range []struct {
		s    crypt.Sealed
		want bool
	}{{old, true}, {cur, false}} {
		c := tc.s.Ciphertext()
		c[len(c)-1] ^= 1
		damaged := crypt.FromCiphertext(c)
		if got := kr.NeedsReseal(damaged); got != tc.want {
			t.Fatalf("NeedsReseal damaged = %v; want %v", got, tc.want)
		}
		r, changed, err := kr.Reseal(damaged, bind)
		if !errors.Is(err, crypt.ErrCannotOpen) || changed || !r.IsZero() {
			t.Fatalf("Reseal damaged = zero %v, changed %v, %v", r.IsZero(), changed, err)
		}
	}
}

func TestOwnershipKeepsValuesOpenable(t *testing.T) {
	kr := newKeyring(t, mustKey(t, crypt.XChaCha20Poly1305))
	pt := []byte("ownership")
	s := sealForKeyring(t, kr, pt, bind)
	buf := s.Ciphertext()
	from := crypt.FromCiphertext(buf)
	buf[12] ^= 1
	requireKeyringPlaintext(t, kr, from, pt, bind)
	buf = s.Ciphertext()
	var scanned crypt.Sealed
	if err := scanned.Scan(buf); err != nil {
		t.Fatal(err)
	}
	copy(buf, sealForKeyring(t, kr, []byte("different"), bind).Ciphertext())
	requireKeyringPlaintext(t, kr, scanned, pt, bind)
	s.Ciphertext()[12] ^= 1
	value, err := s.Value()
	if err != nil {
		t.Fatal(err)
	}
	value.([]byte)[12] ^= 1
	requireKeyringPlaintext(t, kr, s, pt, bind)
	r, changed, err := kr.Reseal(s, bind)
	if err != nil || changed {
		t.Fatalf("Reseal current = %v, %v", changed, err)
	}
	value, err = r.Value()
	if err != nil {
		t.Fatal(err)
	}
	value.([]byte)[12] ^= 1
	s.Ciphertext()[12] ^= 1
	requireKeyringPlaintext(t, kr, s, pt, bind)
	requireKeyringPlaintext(t, kr, r, pt, bind)
}

func TestKeyringNeverPrintsMaterial(t *testing.T) {
	x, aes := mustKey(t, crypt.XChaCha20Poly1305), mustKey(t, crypt.AES256GCM)
	kr := newKeyring(t, x, aes)
	for _, k := range []crypt.Key{x, aes} {
		encoded := strings.SplitN(k.Spec(), ":", 2)[1]
		material, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			t.Fatal(err)
		}
		for _, format := range []string{"%v", "%+v", "%#v"} {
			for _, value := range []any{kr, *kr} {
				printed := fmt.Sprintf(format, value)
				if strings.Contains(printed, encoded) || strings.Contains(printed, hex.EncodeToString(material)) || strings.Contains(printed, fmt.Sprint(material)) {
					t.Fatalf("format %s exposes key material", format)
				}
			}
		}
	}
}

func TestKeyringConcurrentSealOpen(t *testing.T) {
	for _, alg := range []crypt.Algorithm{crypt.AES256GCM, crypt.XChaCha20Poly1305} {
		kr := newKeyring(t, mustKey(t, alg))
		var wg sync.WaitGroup
		for worker := 0; worker < 16; worker++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for i := 0; i < 20; i++ {
					pt := []byte("concurrent")
					s, err := kr.Seal(pt, bind)
					if err != nil {
						t.Error(err)
						return
					}
					got, err := kr.Open(s, bind)
					if err != nil || !bytes.Equal(got, pt) {
						t.Errorf("concurrent Open = %q, %v", got, err)
						return
					}
				}
			}()
		}
		wg.Wait()
	}
}
