package bunstore_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/borfast/sulis"
	"github.com/uptrace/bun"

	"github.com/borfast/gorfast/auth/bunstore"
	"github.com/borfast/gorfast/internal/testdb"
)

const (
	aliceResetHash = "alice-reset-hash"
	bobResetHash   = "bob-reset-hash"
	magicLinkHash  = "magic-link-hash"
	resetPurpose   = sulis.TokenPurpose("reset")
	magicPurpose   = sulis.TokenPurpose("magic_link")
)

// seedDeleteUser creates alice and bob with sessions and tokens, plus one
// magic-link token that belongs to no user yet.
func seedDeleteUser(t *testing.T, db bun.IDB) {
	t.Helper()
	ctx := t.Context()
	now := time.Now().UTC()
	later := now.Add(time.Hour)
	users := bunstore.NewUserStore(db)
	sessions := bunstore.NewSessionStore(db)
	tokens := bunstore.NewTokenStore(db)

	for _, u := range []struct{ id, email string }{{"alice", "alice@example.test"}, {"bob", "bob@example.test"}} {
		if err := users.CreateUser(ctx, &sulis.User{ID: u.id, Email: u.email, CreatedAt: now, UpdatedAt: now}); err != nil {
			t.Fatalf("create %s: %v", u.id, err)
		}
	}
	for _, s := range []struct{ id, user string }{{"s1", "alice"}, {"s2", "alice"}, {"s3", "bob"}} {
		session := &sulis.Session{ID: s.id, UserID: s.user, TokenHash: s.id + "-hash", ExpiresAt: later, CreatedAt: now, AuthenticatedAt: now, LastSeenAt: now}
		if err := sessions.CreateSession(ctx, session); err != nil {
			t.Fatalf("create session %s: %v", s.id, err)
		}
	}
	for _, tk := range []struct {
		id, user, hash, email string
		purpose               sulis.TokenPurpose
	}{
		{"t1", "alice", aliceResetHash, "", resetPurpose},
		{"t2", "bob", bobResetHash, "", resetPurpose},
		{"t3", "", magicLinkHash, "alice@example.test", magicPurpose},
	} {
		token := &sulis.Token{ID: tk.id, UserID: tk.user, TokenHash: tk.hash, Purpose: tk.purpose, ExpiresAt: later, CreatedAt: now, Email: tk.email}
		if err := tokens.CreateToken(ctx, token); err != nil {
			t.Fatalf("create token %s: %v", tk.id, err)
		}
	}
}

func requireSessionCount(t *testing.T, sessions sulis.SessionStore, userID string, want int) {
	t.Helper()
	got, err := sessions.ListUserSessions(t.Context(), userID)
	if err != nil {
		t.Fatalf("list sessions for %s: %v", userID, err)
	}
	if len(got) != want {
		t.Fatalf("sessions for %s = %d, want %d", userID, len(got), want)
	}
}

func TestDeleteUserRemovesSessionsAndTokens(t *testing.T) {
	testdb.Each(t, func(t *testing.T, db *bun.DB) {
		ctx := t.Context()
		seedDeleteUser(t, db)
		users, sessions, tokens := bunstore.NewUserStore(db), bunstore.NewSessionStore(db), bunstore.NewTokenStore(db)

		if err := users.DeleteUser(ctx, "alice"); err != nil {
			t.Fatalf("DeleteUser: %v", err)
		}
		requireSessionCount(t, sessions, "alice", 0)
		requireSessionCount(t, sessions, "bob", 1)
		if _, err := tokens.ConsumeToken(ctx, aliceResetHash, resetPurpose); !errors.Is(err, sulis.ErrTokenNotFound) {
			t.Fatalf("alice's token after delete: %v, want ErrTokenNotFound", err)
		}
		if _, err := tokens.ConsumeToken(ctx, bobResetHash, resetPurpose); err != nil {
			t.Fatalf("bob's token after alice's delete: %v", err)
		}
		if _, err := tokens.ConsumeToken(ctx, magicLinkHash, magicPurpose); err != nil {
			t.Fatalf("token without a user after delete: %v", err)
		}
		if _, err := users.GetUserByID(ctx, "bob"); err != nil {
			t.Fatalf("bob after alice's delete: %v", err)
		}
	})
}

func TestDeleteUserMissingAndEmptyID(t *testing.T) {
	testdb.Each(t, func(t *testing.T, db *bun.DB) {
		ctx := t.Context()
		seedDeleteUser(t, db)
		users, sessions, tokens := bunstore.NewUserStore(db), bunstore.NewSessionStore(db), bunstore.NewTokenStore(db)

		for _, id := range []string{"nobody", ""} {
			if err := users.DeleteUser(ctx, id); err != nil {
				t.Fatalf("DeleteUser(%q): %v", id, err)
			}
		}
		for _, id := range []string{"alice", "bob"} {
			if _, err := users.GetUserByID(ctx, id); err != nil {
				t.Fatalf("%s after no-op deletes: %v", id, err)
			}
		}
		requireSessionCount(t, sessions, "alice", 2)
		requireSessionCount(t, sessions, "bob", 1)
		if _, err := tokens.ConsumeToken(ctx, magicLinkHash, magicPurpose); err != nil {
			t.Fatalf("token without a user after DeleteUser(\"\"): %v", err)
		}
	})
}

func TestDeleteUserInsideCallerTransaction(t *testing.T) {
	testdb.Each(t, func(t *testing.T, db *bun.DB) {
		ctx := t.Context()
		seedDeleteUser(t, db)
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback() }()
		txUsers, txSessions, txTokens := bunstore.NewUserStore(tx), bunstore.NewSessionStore(tx), bunstore.NewTokenStore(tx)

		if err := txUsers.DeleteUser(ctx, "alice"); err != nil {
			t.Fatalf("DeleteUser in tx: %v", err)
		}
		if _, err := txUsers.GetUserByID(ctx, "alice"); !errors.Is(err, sulis.ErrUserNotFound) {
			t.Fatalf("alice through tx: %v, want ErrUserNotFound", err)
		}
		requireSessionCount(t, txSessions, "alice", 0)
		if _, err := txTokens.ConsumeToken(ctx, aliceResetHash, resetPurpose); !errors.Is(err, sulis.ErrTokenNotFound) {
			t.Fatalf("alice's token through tx: %v, want ErrTokenNotFound", err)
		}

		if err := tx.Rollback(); err != nil {
			t.Fatal(err)
		}
		if _, err := bunstore.NewUserStore(db).GetUserByID(ctx, "alice"); err != nil {
			t.Fatalf("alice after rollback: %v", err)
		}
		requireSessionCount(t, bunstore.NewSessionStore(db), "alice", 2)
		if _, err := bunstore.NewTokenStore(db).ConsumeToken(ctx, aliceResetHash, resetPurpose); err != nil {
			t.Fatalf("alice's token after rollback: %v", err)
		}
	})
}

// queryRecorder keeps every SQL statement a bun.DB runs, in order.
type queryRecorder struct{ queries []string }

func (r *queryRecorder) BeforeQuery(ctx context.Context, _ *bun.QueryEvent) context.Context {
	return ctx
}

func (r *queryRecorder) AfterQuery(_ context.Context, e *bun.QueryEvent) {
	r.queries = append(r.queries, e.Query)
}

// The user row goes first so a login racing the delete fails on the missing
// user instead of leaving a fresh orphan session behind.
func TestDeleteUserRemovesTheUserRowFirst(t *testing.T) {
	testdb.Each(t, func(t *testing.T, db *bun.DB) {
		seedDeleteUser(t, db)
		rec := &queryRecorder{}
		db.AddQueryHook(rec)
		if err := bunstore.NewUserStore(db).DeleteUser(t.Context(), "alice"); err != nil {
			t.Fatalf("DeleteUser: %v", err)
		}
		first := map[string]int{}
		for i, q := range rec.queries {
			for _, table := range []string{"users", "sessions", "tokens"} {
				if _, seen := first[table]; !seen && strings.HasPrefix(q, "DELETE FROM") && strings.Contains(q, "\""+table+"\"") {
					first[table] = i
				}
			}
		}
		if len(first) != 3 {
			t.Fatalf("saw deletes for %v; want users, sessions and tokens in %q", first, rec.queries)
		}
		if first["users"] > first["sessions"] || first["users"] > first["tokens"] {
			t.Fatalf("users deleted at statement %d, after sessions %d or tokens %d", first["users"], first["sessions"], first["tokens"])
		}
	})
}
