package crypt_test

import (
	"testing"

	"github.com/borfast/gorfast/crypt"
	"github.com/borfast/gorfast/crypt/crypttest"
)

func TestKeyringConformance(t *testing.T) {
	for _, tc := range []struct {
		name string
		alg  crypt.Algorithm
	}{
		{"xchacha20poly1305", crypt.XChaCha20Poly1305},
		{"aes256gcm", crypt.AES256GCM},
	} {
		t.Run(tc.name, func(t *testing.T) {
			crypttest.RunEncryptor(t, func() crypt.Encryptor {
				return newKeyring(t, mustKey(t, tc.alg))
			})
		})
	}
	t.Run("with retired key", func(t *testing.T) {
		crypttest.RunEncryptor(t, func() crypt.Encryptor {
			return newKeyring(t, mustKey(t, crypt.XChaCha20Poly1305), mustKey(t, crypt.AES256GCM))
		})
	})
}

func TestStaticKeysConformance(t *testing.T) {
	a, b := mustKey(t, crypt.XChaCha20Poly1305), mustKey(t, crypt.AES256GCM)
	crypttest.RunKeySource(t, func() crypt.KeySource {
		return crypt.StaticKeys(a.Spec(), b.Spec())
	})
}
