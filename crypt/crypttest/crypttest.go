// Package crypttest provides conformance suites for crypt implementations.
package crypttest

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/borfast/gorfast/crypt"
)

// RunEncryptor checks the contract that every crypt.Encryptor must satisfy.
// It creates one implementation per subtest using factory.
func RunEncryptor(t *testing.T, factory func() crypt.Encryptor) {
	t.Helper()
	t.Run("RoundTrip", func(t *testing.T) {
		e := factory()
		allBytes := make([]byte, 256)
		for i := range allBytes {
			allBytes[i] = byte(i)
		}
		random := make([]byte, 64*1024)
		if _, err := rand.Read(random); err != nil {
			t.Fatal(err)
		}
		for i, plaintext := range [][]byte{nil, []byte("hello"), allBytes, random} {
			for j, binding := range [][]byte{nil, crypt.Bind("messages", "body", "42")} {
				s := requireSeal(t, e, plaintext, binding)
				got, err := e.Open(s, binding)
				if err != nil || !bytes.Equal(got, plaintext) {
					t.Errorf("plaintext %d, binding %d: Open returned %d bytes, %v; want original plaintext", i, j, len(got), err)
				}
			}
		}
	})
	t.Run("FreshCiphertextEachSeal", func(t *testing.T) {
		e := factory()
		binding := crypt.Bind("messages", "body", "42")
		a := requireSeal(t, e, []byte("hello"), binding)
		b := requireSeal(t, e, []byte("hello"), binding)
		if bytes.Equal(a.Ciphertext(), b.Ciphertext()) {
			t.Error("repeated Seal returned the same ciphertext")
		}
	})
	t.Run("EmptyPlaintext", func(t *testing.T) {
		e := factory()
		binding := crypt.Bind("messages", "body", "42")
		s := requireSeal(t, e, []byte{}, binding)
		plaintext, err := e.Open(s, binding)
		if err != nil || len(plaintext) != 0 {
			t.Errorf("Open returned %d bytes, %v; want empty plaintext", len(plaintext), err)
		}
	})
	t.Run("WrongBinding", func(t *testing.T) {
		e := factory()
		s := requireSeal(t, e, []byte("hello"), crypt.Bind("messages", "body", "42"))
		for i, binding := range [][]byte{
			crypt.Bind("messages", "body", "43"), nil, crypt.Bind("messages", "body42"),
		} {
			t.Logf("wrong binding %d", i)
			requireOpenFails(t, e, s, binding)
		}
	})
	t.Run("FlippedByte", func(t *testing.T) {
		e := factory()
		binding := crypt.Bind("messages", "body", "42")
		ciphertext := requireSeal(t, e, []byte("hello"), binding).Ciphertext()
		for i := range ciphertext {
			for _, mask := range []byte{0x01, 0x80} {
				changed := bytes.Clone(ciphertext)
				changed[i] ^= mask
				t.Logf("byte %d, mask %#x", i, mask)
				requireOpenFails(t, e, crypt.FromCiphertext(changed), binding)
			}
		}
	})
	t.Run("Truncated", func(t *testing.T) {
		e := factory()
		binding := crypt.Bind("messages", "body", "42")
		ciphertext := requireSeal(t, e, []byte("hello"), binding).Ciphertext()
		for n := 0; n < len(ciphertext); n++ {
			t.Logf("prefix length %d", n)
			requireOpenFails(t, e, crypt.FromCiphertext(ciphertext[:n]), binding)
		}
	})
	t.Run("Extended", func(t *testing.T) {
		e := factory()
		binding := crypt.Bind("messages", "body", "42")
		ciphertext := requireSeal(t, e, []byte("hello"), binding).Ciphertext()
		requireOpenFails(t, e, crypt.FromCiphertext(append(ciphertext, 0x00)), binding)
	})
	t.Run("InvalidInputs", func(t *testing.T) {
		e := factory()
		random := make([]byte, 64)
		if _, err := rand.Read(random); err != nil {
			t.Fatal(err)
		}
		for i, s := range []crypt.Sealed{
			{}, crypt.FromCiphertext([]byte{}), crypt.FromCiphertext([]byte{0x01}), crypt.FromCiphertext(random),
		} {
			t.Logf("invalid input %d", i)
			requireOpenFails(t, e, s, crypt.Bind("messages", "body", "42"))
		}
	})
	t.Run("CiphertextRoundTrip", func(t *testing.T) {
		e := factory()
		binding := crypt.Bind("messages", "body", "42")
		plaintext := []byte("hello")
		s := requireSeal(t, e, plaintext, binding)
		got, err := e.Open(crypt.FromCiphertext(s.Ciphertext()), binding)
		if err != nil || !bytes.Equal(got, plaintext) {
			t.Errorf("Open after ciphertext export returned %d bytes, %v; want original plaintext", len(got), err)
		}
	})
	t.Run("ConcurrentUse", func(t *testing.T) {
		e := factory()
		var wg sync.WaitGroup
		for worker := 0; worker < 8; worker++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				binding := crypt.Bind("messages", "body", fmt.Sprint(worker))
				for iteration := 0; iteration < 100; iteration++ {
					plaintext := []byte(fmt.Sprintf("worker %d, iteration %d", worker, iteration))
					s, err := e.Seal(plaintext, binding)
					if err != nil || s.IsZero() {
						t.Errorf("worker %d, iteration %d: Seal zero=%v, error=%v", worker, iteration, s.IsZero(), err)
						return
					}
					got, err := e.Open(s, binding)
					if err != nil || !bytes.Equal(got, plaintext) {
						t.Errorf("worker %d, iteration %d: Open returned %d bytes, %v; want original plaintext", worker, iteration, len(got), err)
						return
					}
				}
			}()
		}
		wg.Wait()
	})
}

// RunKeySource checks the contract that every crypt.KeySource must satisfy.
// It creates one implementation per subtest using factory.
func RunKeySource(t *testing.T, factory func() crypt.KeySource) {
	t.Helper()
	t.Run("SuppliesUsableKeys", func(t *testing.T) {
		src := factory()
		keys, err := src.Keys(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if len(keys) == 0 {
			t.Fatal("Keys returned no keys")
		}
		if _, err := crypt.Load(context.Background(), src); err != nil {
			t.Fatalf("Load rejected source: %v", err)
		}
	})
	t.Run("StableOrder", func(t *testing.T) {
		src := factory()
		a, err := src.Keys(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		b, err := src.Keys(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if len(a) != len(b) {
			t.Fatalf("Keys returned lengths %d and %d; want equal lengths", len(a), len(b))
		}
		for i := range a {
			if a[i].String() != b[i].String() {
				t.Errorf("key %d changed from %s to %s", i, a[i], b[i])
			}
		}
	})
	t.Run("CancelledContext", func(t *testing.T) {
		src := factory()
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		result := make(chan error, 1)
		go func() {
			_, err := src.Keys(ctx)
			result <- err
		}()
		timer := time.NewTimer(5 * time.Second)
		defer timer.Stop()
		select {
		case err := <-result:
			if err == nil {
				t.Error("Keys succeeded with a cancelled context")
			}
		case <-timer.C:
			t.Error("Keys did not return within 5 seconds with a cancelled context")
		}
	})
}

func requireSeal(t *testing.T, e crypt.Encryptor, plaintext, binding []byte) crypt.Sealed {
	t.Helper()
	s, err := e.Seal(plaintext, binding)
	if err != nil {
		t.Fatalf("Seal failed: %v", err)
	}
	if s.IsZero() {
		t.Fatal("Seal returned a zero value")
	}
	return s
}

func requireOpenFails(t *testing.T, e crypt.Encryptor, s crypt.Sealed, binding []byte) {
	t.Helper()
	plaintext, err := e.Open(s, binding)
	if err == nil {
		t.Error("Open succeeded; want an error")
	}
	if plaintext != nil {
		t.Error("Open returned plaintext; want nil on failure")
	}
	if !errors.Is(err, crypt.ErrCannotOpen) && !errors.Is(err, crypt.ErrUnknownKey) {
		t.Errorf("Open error = %v; want ErrCannotOpen or ErrUnknownKey", err)
	}
}
