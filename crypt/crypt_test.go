package crypt_test

import (
	"bytes"
	"testing"

	"github.com/borfast/gorfast/crypt"
)

func TestBindIsUnambiguous(t *testing.T) {
	pairs := [][2][]byte{
		{crypt.Bind("ab", "c"), crypt.Bind("a", "bc")},
		{crypt.Bind(""), crypt.Bind()},
		{crypt.Bind("", ""), crypt.Bind("")},
	}
	for _, p := range pairs {
		if bytes.Equal(p[0], p[1]) {
			t.Errorf("Bind collision: %x", p[0])
		}
	}
	want := []byte{0, 0, 0, 1, 'a', 0, 0, 0, 2, 'b', 'c'}
	if got := crypt.Bind("a", "bc"); !bytes.Equal(got, want) {
		t.Errorf("got %x, want %x", got, want)
	}
	if len(crypt.Bind()) != 0 {
		t.Error("Bind() is not empty")
	}
}

func TestEmptyIsNotZero(t *testing.T) {
	if !(crypt.Sealed{}).IsZero() || !crypt.FromCiphertext(nil).IsZero() {
		t.Error("NULL forms must be zero")
	}
	if got := (crypt.Sealed{}).Ciphertext(); got != nil {
		t.Errorf("zero ciphertext: %#v", got)
	}
	s := crypt.FromCiphertext([]byte{})
	if s.IsZero() || s.Ciphertext() == nil {
		t.Error("empty bytes are invalid, not absent")
	}
}

func TestFromCiphertextCopies(t *testing.T) {
	buf := []byte{1, 2, 3}
	s := crypt.FromCiphertext(buf)
	buf[0] = 9
	c := s.Ciphertext()
	c[1] = 9
	if got := s.Ciphertext(); !bytes.Equal(got, []byte{1, 2, 3}) {
		t.Errorf("got %x", got)
	}
}
