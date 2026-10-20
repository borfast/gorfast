package main

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
	"uuid"

	"github.com/borfast/sulis"

	"github.com/borfast/gorfast/auth/bunstore"
	"github.com/borfast/gorfast/crypt"
	"github.com/borfast/gorfast/examples/todo/internal/storage"
)

// syncBuffer is a log destination that the server and the test use at once.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func newKeySpec(t *testing.T) string {
	t.Helper()
	key, err := crypt.GenerateKey(crypt.XChaCha20Poly1305)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	return key.Spec()
}

// setEnv sets every TODO_ variable and returns an env file path that does not exist.
func setEnv(t *testing.T, keys, level string) string {
	t.Helper()
	t.Setenv("TODO_SERVER__ADDR", "127.0.0.1:0")
	t.Setenv("TODO_DATABASE__URL", filepath.Join(t.TempDir(), "todo.db"))
	t.Setenv("TODO_CRYPT__KEYS", keys)
	t.Setenv("TODO_LOG__LEVEL", level)
	return filepath.Join(t.TempDir(), "missing.env")
}

func TestRunRefusesToStartWithoutAKey(t *testing.T) {
	envFile := setEnv(t, "", "info")
	err := run(t.Context(), envFile, &syncBuffer{})
	if err == nil {
		t.Fatal("run returned nil without a key")
	}
	for _, want := range []string{"TODO_CRYPT__KEYS", "crypt/cmd/newkey"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not contain %q", err, want)
		}
	}
}

func TestRunRefusesAMalformedKey(t *testing.T) {
	envFile := setEnv(t, "xchacha20poly1305:not-base64-secret", "info")
	err := run(t.Context(), envFile, &syncBuffer{})
	if err == nil {
		t.Fatal("run returned nil with a malformed key")
	}
	if !strings.Contains(err.Error(), "TODO_CRYPT__KEYS") {
		t.Errorf("error %q does not name TODO_CRYPT__KEYS", err)
	}
	if strings.Contains(err.Error(), "not-base64-secret") {
		t.Errorf("error %q contains the key material", err)
	}
}

func TestRunRefusesAnUnknownLogLevel(t *testing.T) {
	envFile := setEnv(t, "", "loud")
	err := run(t.Context(), envFile, &syncBuffer{})
	if err == nil {
		t.Fatal("run returned nil with an unknown log level")
	}
	for _, want := range []string{"TODO_LOG__LEVEL", "TODO_CRYPT__KEYS"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not contain %q", err, want)
		}
	}
}

func TestRunServesAndStops(t *testing.T) {
	envFile := setEnv(t, newKeySpec(t), "info")
	logs := &syncBuffer{}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- run(ctx, envFile, logs) }()

	addrPattern := regexp.MustCompile(`addr=(127\.0\.0\.1:\d+)`)
	var addr string
	deadline := time.Now().Add(10 * time.Second)
	for addr == "" {
		if m := addrPattern.FindStringSubmatch(logs.String()); m != nil {
			addr = m[1]
			break
		}
		select {
		case err := <-done:
			t.Fatalf("run stopped before it listened: %v\n%s", err, logs.String())
		case <-time.After(20 * time.Millisecond):
		}
		if time.Now().After(deadline) {
			t.Fatalf("run never logged an address:\n%s", logs.String())
		}
	}

	resp, err := http.Get("http://" + addr + "/signin")
	if err != nil {
		t.Fatalf("GET /signin: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("GET /signin status = %d, want 200", resp.StatusCode)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("run returned %v after cancel, want nil", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("run did not stop after cancel")
	}
}

func TestRunRefusesAMalformedPostgresURLWithoutLeakingIt(t *testing.T) {
	envFile := setEnv(t, newKeySpec(t), "info")
	t.Setenv("TODO_DATABASE__URL", "postgres://app:s3cretpw@db.example:54x2/todo")
	err := run(t.Context(), envFile, &syncBuffer{})
	if err == nil {
		t.Fatal("run returned nil with a malformed database URL")
	}
	if !strings.Contains(err.Error(), "TODO_DATABASE__URL") {
		t.Errorf("error %q does not name TODO_DATABASE__URL", err)
	}
	if strings.Contains(err.Error(), "s3cretpw") {
		t.Errorf("error %q contains the password", err)
	}
}

func TestRunDeletesExpiredSessions(t *testing.T) {
	envFile := setEnv(t, newKeySpec(t), "info")
	db, _, err := openDatabase(os.Getenv("TODO_DATABASE__URL"))
	if err != nil {
		t.Fatalf("openDatabase: %v", err)
	}
	defer db.Close()
	if err := storage.Apply(t.Context(), db); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	sessions := bunstore.NewSessionStore(db)
	now := time.Now().UTC()
	for hash, expires := range map[string]time.Time{"expired": now.Add(-time.Hour), "live": now.Add(time.Hour)} {
		session := &sulis.Session{
			ID: uuid.NewV7(), UserID: uuid.NewV7(), TokenHash: hash, ExpiresAt: expires,
			CreatedAt: now.Add(-2 * time.Hour), AuthenticatedAt: now.Add(-2 * time.Hour), LastSeenAt: now.Add(-2 * time.Hour),
			IP: "192.0.2.1", UserAgent: "test",
		}
		if err := sessions.CreateSession(t.Context(), session); err != nil {
			t.Fatalf("CreateSession %s: %v", hash, err)
		}
	}

	logs := &syncBuffer{}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- run(ctx, envFile, logs) }()

	deadline := time.Now().Add(10 * time.Second)
	for {
		_, err := sessions.GetSessionByTokenHash(t.Context(), "expired")
		if errors.Is(err, sulis.ErrSessionNotFound) {
			break
		}
		if err != nil {
			t.Fatalf("GetSessionByTokenHash: %v", err)
		}
		select {
		case err := <-done:
			t.Fatalf("run stopped with the expired session still there: %v\n%s", err, logs.String())
		case <-time.After(20 * time.Millisecond):
		}
		if time.Now().After(deadline) {
			t.Fatalf("the expired session is still there:\n%s", logs.String())
		}
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("run returned %v after cancel, want nil", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("run did not stop after cancel")
	}
	if _, err := sessions.GetSessionByTokenHash(t.Context(), "live"); err != nil {
		t.Errorf("the live session: %v, want it kept", err)
	}
}
