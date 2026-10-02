package crypt

import (
	"crypto/aes"
	"crypto/cipher"

	"golang.org/x/crypto/chacha20poly1305"
)

type algorithm struct {
	name      string
	nonceSize int
	newAEAD   func(key []byte) (cipher.AEAD, error)
}

var algorithms = map[Algorithm]algorithm{
	AES256GCM: {
		name:      "aes256gcm",
		nonceSize: 12,
		newAEAD: func(key []byte) (cipher.AEAD, error) {
			block, err := aes.NewCipher(key)
			if err != nil {
				return nil, err
			}
			return cipher.NewGCM(block)
		},
	},
	XChaCha20Poly1305: {
		name:      "xchacha20poly1305",
		nonceSize: 24,
		newAEAD:   chacha20poly1305.NewX,
	},
}

func lookupAlgorithm(a Algorithm) (algorithm, bool) {
	alg, ok := algorithms[a]
	return alg, ok
}
