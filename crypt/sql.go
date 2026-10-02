package crypt

import (
	"database/sql/driver"
	"fmt"
)

// Value returns SQL NULL for the zero value, otherwise a copy of ciphertext.
func (s Sealed) Value() (driver.Value, error) {
	if s.IsZero() {
		return nil, nil
	}
	return s.Ciphertext(), nil
}

// Scan copies raw ciphertext from a SQL value, preserving NULL and empty bytes.
// An unsupported source leaves the previous value unchanged.
func (s *Sealed) Scan(src any) error {
	switch src := src.(type) {
	case nil:
		s.b = nil
	case []byte:
		s.b = make([]byte, len(src))
		copy(s.b, src)
	case string:
		s.b = []byte(src)
	default:
		return fmt.Errorf("crypt: cannot scan %T into Sealed", src)
	}
	return nil
}
