package crypt_test

import (
	"bytes"
	"database/sql"
	"database/sql/driver"
	"errors"
	"strings"
	"testing"

	"github.com/borfast/gorfast/crypt"
)

var (
	_ driver.Valuer = crypt.Sealed{}
	_ sql.Scanner   = (*crypt.Sealed)(nil)
)

func TestValue(t *testing.T) {
	if v, err := (crypt.Sealed{}).Value(); v != nil || err != nil {
		t.Errorf("zero: %v, %v", v, err)
	}
	v, err := crypt.FromCiphertext([]byte{}).Value()
	if err != nil {
		t.Fatal(err)
	}
	if b, ok := v.([]byte); !ok || b == nil || len(b) != 0 {
		t.Errorf("empty: %#v", v)
	}
	s := crypt.FromCiphertext([]byte{1, 2, 3})
	v, err = s.Value()
	if err != nil {
		t.Fatal(err)
	}
	v.([]byte)[0] = 9
	if !bytes.Equal(s.Ciphertext(), []byte{1, 2, 3}) {
		t.Error("Value aliases the Sealed")
	}
}

func TestScanCopiesDriverBuffer(t *testing.T) {
	buf := []byte{1, 2, 3}
	var s crypt.Sealed
	if err := s.Scan(buf); err != nil {
		t.Fatal(err)
	}
	copy(buf, []byte{7, 7, 7})
	if !bytes.Equal(s.Ciphertext(), []byte{1, 2, 3}) {
		t.Error("Scan aliases the driver buffer")
	}
}

func TestScanSources(t *testing.T) {
	var s crypt.Sealed
	if err := s.Scan("\x01\x02"); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(s.Ciphertext(), []byte{1, 2}) {
		t.Errorf("string is not raw bytes: %x", s.Ciphertext())
	}
	if err := s.Scan([]byte{}); err != nil {
		t.Fatal(err)
	}
	if s.IsZero() || s.Ciphertext() == nil || len(s.Ciphertext()) != 0 {
		t.Error("empty slice must stay non-NULL")
	}
	s = crypt.FromCiphertext([]byte{5})
	err := s.Scan(int64(1))
	if err == nil || !strings.Contains(err.Error(), "int64") {
		t.Errorf("unsupported source error: %v", err)
	}
	if !bytes.Equal(s.Ciphertext(), []byte{5}) {
		t.Error("unsupported source changed the previous value")
	}
	if err := s.Scan([]byte(nil)); err != nil || s.IsZero() {
		t.Errorf("nil slice: %x, %v", s.Ciphertext(), err)
	}
}

func TestScanEmptyBytesStayNonNull(t *testing.T) {
	kr := newKeyring(t, mustKey(t, crypt.XChaCha20Poly1305))
	for _, tc := range []struct {
		name string
		src  []byte
	}{
		{"typed nil", nil},
		{"empty slice", []byte{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := crypt.FromCiphertext([]byte{1, 2, 3})
			if err := s.Scan(tc.src); err != nil {
				t.Fatal(err)
			}
			if s.IsZero() || s.Ciphertext() == nil || len(s.Ciphertext()) != 0 {
				t.Fatal("empty bytes must remain non-NULL and replace previous ciphertext")
			}
			v, err := s.Value()
			if err != nil {
				t.Fatal(err)
			}
			if b, ok := v.([]byte); !ok || b == nil || len(b) != 0 {
				t.Fatalf("empty bytes Value = %#v; want non-nil empty []byte", v)
			}
			if _, err := kr.Open(s, nil); !errors.Is(err, crypt.ErrCannotOpen) {
				t.Fatalf("Open empty bytes = %v; want ErrCannotOpen", err)
			}
		})
	}
}

func TestScanNullResets(t *testing.T) {
	s := crypt.FromCiphertext([]byte{1, 2, 3})
	if err := s.Scan(nil); err != nil || !s.IsZero() {
		t.Errorf("after NULL: %x, %v", s.Ciphertext(), err)
	}
}
