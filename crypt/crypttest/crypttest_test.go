package crypttest_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/borfast/gorfast/crypt"
	"github.com/borfast/gorfast/crypt/crypttest"
)

func TestRunEncryptorCatchesBrokenImplementations(t *testing.T) {
	mustFail := map[string]string{
		"ignores-binding":    "WrongBinding",
		"repeats-ciphertext": "FreshCiphertextEachSeal",
		"plain-errors":       "FlippedByte",
	}
	for name, subtest := range mustFail {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestBrokenHelper$", "-test.v")
			cmd.Env = append(os.Environ(), "CRYPTTEST_BROKEN="+name)
			out, err := cmd.CombinedOutput()
			var exitErr *exec.ExitError
			if !errors.As(err, &exitErr) || exitErr.ExitCode() != 1 {
				t.Fatalf("child error = %v; want test failure with exit 1\n%s", err, out)
			}
			failure := "--- FAIL: TestBrokenHelper/" + subtest
			if !strings.Contains(string(out), failure) {
				t.Fatalf("child did not fail %s:\n%s", subtest, out)
			}
			t.Log(failure)
		})
	}
}

func TestBrokenHelper(t *testing.T) {
	name := os.Getenv("CRYPTTEST_BROKEN")
	if name == "" {
		t.Skip("run by TestRunEncryptorCatchesBrokenImplementations")
	}
	kr := keyring(t)
	crypttest.RunEncryptor(t, func() crypt.Encryptor {
		return &brokenEncryptor{name: name, kr: kr, ciphertexts: make(map[string]crypt.Sealed)}
	})
}

func TestRunEncryptorFactoryPerSubtest(t *testing.T) {
	calls := 0
	kr := keyring(t)
	crypttest.RunEncryptor(t, func() crypt.Encryptor {
		calls++
		return kr
	})
	if calls != 10 {
		t.Fatalf("factory calls = %d; want 10", calls)
	}
}

func TestRunKeySourceFactoryPerSubtest(t *testing.T) {
	calls := 0
	key, err := crypt.GenerateKey(crypt.XChaCha20Poly1305)
	if err != nil {
		t.Fatal(err)
	}
	crypttest.RunKeySource(t, func() crypt.KeySource {
		calls++
		return crypt.StaticKeys(key.Spec())
	})
	if calls != 3 {
		t.Fatalf("factory calls = %d; want 3", calls)
	}
}

func TestRunKeySourceCatchesReorderedSharedSlice(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestReorderingSourceHelper$", "-test.v")
	cmd.Env = append(os.Environ(), "CRYPTTEST_REORDERING_SOURCE=1")
	out, err := cmd.CombinedOutput()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 1 {
		t.Fatalf("child error = %v; want test failure with exit 1\n%s", err, out)
	}
	failure := "--- FAIL: TestReorderingSourceHelper/StableOrder"
	if !strings.Contains(string(out), failure) {
		t.Fatalf("child did not fail StableOrder:\n%s", out)
	}
	t.Log(failure)
}

func TestReorderingSourceHelper(t *testing.T) {
	if os.Getenv("CRYPTTEST_REORDERING_SOURCE") == "" {
		t.Skip("run by TestRunKeySourceCatchesReorderedSharedSlice")
	}
	a, err := crypt.GenerateKey(crypt.XChaCha20Poly1305)
	if err != nil {
		t.Fatal(err)
	}
	b, err := crypt.GenerateKey(crypt.AES256GCM)
	if err != nil {
		t.Fatal(err)
	}
	if a.String() == b.String() {
		t.Fatal("regression fixture requires distinct key IDs")
	}
	crypttest.RunKeySource(t, func() crypt.KeySource {
		return &reorderingKeySource{keys: []crypt.Key{a, b}}
	})
}

type reorderingKeySource struct {
	keys []crypt.Key
}

func (s *reorderingKeySource) Keys(ctx context.Context) ([]crypt.Key, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.keys[0], s.keys[1] = s.keys[1], s.keys[0]
	return s.keys, nil
}

func keyring(t *testing.T) *crypt.Keyring {
	t.Helper()
	key, err := crypt.GenerateKey(crypt.XChaCha20Poly1305)
	if err != nil {
		t.Fatal(err)
	}
	kr, err := crypt.NewKeyring(key)
	if err != nil {
		t.Fatal(err)
	}
	return kr
}

type brokenEncryptor struct {
	name        string
	kr          *crypt.Keyring
	mu          sync.Mutex
	ciphertexts map[string]crypt.Sealed
}

func (e *brokenEncryptor) Seal(plaintext, binding []byte) (crypt.Sealed, error) {
	if e.name == "ignores-binding" {
		binding = nil
	}
	if e.name == "repeats-ciphertext" {
		e.mu.Lock()
		defer e.mu.Unlock()
		if s, ok := e.ciphertexts[string(plaintext)]; ok {
			return s, nil
		}
		s, err := e.kr.Seal(plaintext, binding)
		if err == nil {
			e.ciphertexts[string(plaintext)] = s
		}
		return s, err
	}
	return e.kr.Seal(plaintext, binding)
}

func (e *brokenEncryptor) Open(s crypt.Sealed, binding []byte) ([]byte, error) {
	if e.name == "ignores-binding" {
		binding = nil
	}
	plaintext, err := e.kr.Open(s, binding)
	if e.name == "plain-errors" && err != nil {
		return plaintext, errors.New("open failed")
	}
	return plaintext, err
}
