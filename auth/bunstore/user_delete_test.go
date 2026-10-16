package bunstore_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/borfast/sulis"
	"github.com/borfast/sulis/totp"
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

// seedSecondFactors gives alice and bob each an active TOTP credential, a
// passkey and the recovery codes h1 and h2.
func seedSecondFactors(t *testing.T, db bun.IDB) {
	t.Helper()
	ctx := t.Context()
	now := time.Now().UTC().Truncate(time.Second)
	totps, passkeys, recoveries := bunstore.NewTOTPStore(db), bunstore.NewPasskeyStore(db), bunstore.NewRecoveryStore(db)

	for _, user := range []string{"alice", "bob"} {
		activeTOTP(t, totps, user, user+"-totp", 1)
		if err := passkeys.SaveCredential(ctx, newPasskey(user, user+"-passkey", now)); err != nil {
			t.Fatalf("save passkey for %s: %v", user, err)
		}
		if err := recoveries.ReplaceCodes(ctx, user, []string{"h1", "h2"}); err != nil {
			t.Fatalf("replace codes for %s: %v", user, err)
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

func TestDeleteUserRemovesSecondFactors(t *testing.T) {
	testdb.Each(t, func(t *testing.T, db *bun.DB) {
		ctx := t.Context()
		seedDeleteUser(t, db)
		seedSecondFactors(t, db)
		totps, passkeys, recoveries := bunstore.NewTOTPStore(db), bunstore.NewPasskeyStore(db), bunstore.NewRecoveryStore(db)

		if err := bunstore.NewUserStore(db).DeleteUser(ctx, "alice"); err != nil {
			t.Fatalf("DeleteUser: %v", err)
		}

		if _, err := totps.GetActiveTOTP(ctx, "alice"); !errors.Is(err, totp.ErrTOTPNotEnrolled) {
			t.Fatalf("alice's TOTP after delete: %v, want ErrTOTPNotEnrolled", err)
		}
		creds, err := passkeys.GetCredentialsByUserID(ctx, "alice")
		if err != nil || len(creds) != 0 {
			t.Fatalf("alice's passkeys after delete = %d, %v; want 0", len(creds), err)
		}
		if n, err := recoveries.CountCodes(ctx, "alice"); err != nil || n != 0 {
			t.Fatalf("alice's codes after delete = %d, %v; want 0", n, err)
		}
		if n := countRows(t, db, "recovery_code_sets", "alice"); n != 0 {
			t.Fatalf("alice's recovery_code_sets rows = %d, want 0", n)
		}

		if _, err := totps.GetActiveTOTP(ctx, "bob"); err != nil {
			t.Fatalf("bob's TOTP after alice's delete: %v", err)
		}
		creds, err = passkeys.GetCredentialsByUserID(ctx, "bob")
		if err != nil || len(creds) != 1 {
			t.Fatalf("bob's passkeys after alice's delete = %d, %v; want 1", len(creds), err)
		}
		if n, err := recoveries.CountCodes(ctx, "bob"); err != nil || n != 2 {
			t.Fatalf("bob's codes after alice's delete = %d, %v; want 2", n, err)
		}
		if n := countRows(t, db, "recovery_code_sets", "bob"); n != 1 {
			t.Fatalf("bob's recovery_code_sets rows = %d, want 1", n)
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
		seedSecondFactors(t, db)
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
		if _, err := bunstore.NewTOTPStore(db).GetActiveTOTP(ctx, "alice"); err != nil {
			t.Fatalf("alice's TOTP after rollback: %v", err)
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
// user instead of leaving a fresh orphan session behind. The set row goes
// before the codes to keep the lock order in the design's section 6.
func TestDeleteUserDeleteOrder(t *testing.T) {
	testdb.Each(t, func(t *testing.T, db *bun.DB) {
		seedDeleteUser(t, db)
		seedSecondFactors(t, db)
		rec := &queryRecorder{}
		db.AddQueryHook(rec)
		if err := bunstore.NewUserStore(db).DeleteUser(t.Context(), "alice"); err != nil {
			t.Fatalf("DeleteUser: %v", err)
		}
		want := []string{"users", "sessions", "tokens", "totp_credentials", "passkey_credentials", "recovery_code_sets", "recovery_codes"}
		first := map[string]int{}
		for i, q := range rec.queries {
			for _, table := range want {
				if _, seen := first[table]; !seen && strings.HasPrefix(q, "DELETE FROM") && strings.Contains(q, "\""+table+"\"") {
					first[table] = i
				}
			}
		}
		if len(first) != len(want) {
			t.Fatalf("saw deletes for %v; want %v in %q", first, want, rec.queries)
		}
		for i := 1; i < len(want); i++ {
			if first[want[i-1]] >= first[want[i]] {
				t.Fatalf("%s deleted at statement %d, not before %s at %d", want[i-1], first[want[i-1]], want[i], first[want[i]])
			}
		}
	})
}
