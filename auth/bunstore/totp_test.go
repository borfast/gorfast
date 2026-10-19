package bunstore_test

import (
	"errors"
	"math"
	"testing"
	"time"

	"github.com/borfast/sulis/storetest"
	"github.com/borfast/sulis/totp"
	"github.com/uptrace/bun"

	"github.com/borfast/gorfast/auth/bunstore"
	"github.com/borfast/gorfast/internal/testdb"
)

func TestTOTPStore(t *testing.T) {
	testdb.Each(t, func(t *testing.T, db *bun.DB) {
		storetest.RunTOTPStore(t, func() totp.Store {
			testdb.Reset(t, db)
			return bunstore.NewTOTPStore(db)
		})
	})
}

// activeTOTP enrolls a pending credential and promotes it, with or without a
// prior active credential.
func activeTOTP(t *testing.T, s totp.Store, userID, id string, counter uint64) *totp.Credential {
	t.Helper()

	cred := &totp.Credential{
		ID:        id,
		UserID:    userID,
		Secret:    "secret-" + id,
		CreatedAt: time.Now().UTC().Truncate(time.Second),
	}
	if err := s.ReplacePending(t.Context(), cred); err != nil {
		t.Fatalf("ReplacePending: %v", err)
	}
	active, err := s.ConfirmEnrollment(t.Context(), userID, id, counter)
	if err != nil {
		t.Fatalf("ConfirmEnrollment: %v", err)
	}

	return active
}

func TestSaveTOTPWithAReplacedCredentialID(t *testing.T) {
	testdb.Each(t, func(t *testing.T, db *bun.DB) {
		s := bunstore.NewTOTPStore(db)
		old := activeTOTP(t, s, "alice", "old", 10)
		activeTOTP(t, s, "alice", "new", 20)

		old.LastUsedCounter = 30
		if err := s.SaveTOTP(t.Context(), old); !errors.Is(err, totp.ErrTOTPNotEnrolled) {
			t.Fatalf("SaveTOTP = %v, want ErrTOTPNotEnrolled", err)
		}

		got, err := s.GetActiveTOTP(t.Context(), "alice")
		if err != nil {
			t.Fatalf("GetActiveTOTP: %v", err)
		}
		if got.ID != "new" || got.LastUsedCounter != 20 || got.Secret != "secret-new" {
			t.Errorf("active = %+v, want the new credential at counter 20", got)
		}
	})
}

func TestSaveTOTPAfterDeleteTOTP(t *testing.T) {
	testdb.Each(t, func(t *testing.T, db *bun.DB) {
		s := bunstore.NewTOTPStore(db)
		cred := activeTOTP(t, s, "alice", "c1", 10)
		if err := s.DeleteTOTP(t.Context(), "alice"); err != nil {
			t.Fatalf("DeleteTOTP: %v", err)
		}

		cred.LastUsedCounter = 11
		if err := s.SaveTOTP(t.Context(), cred); !errors.Is(err, totp.ErrTOTPNotEnrolled) {
			t.Fatalf("SaveTOTP = %v, want ErrTOTPNotEnrolled", err)
		}
		if _, err := s.GetActiveTOTP(t.Context(), "alice"); !errors.Is(err, totp.ErrTOTPNotEnrolled) {
			t.Fatalf("GetActiveTOTP = %v, want ErrTOTPNotEnrolled", err)
		}
	})
}

func TestSaveTOTPLoweredCounterReturnsErrTOTPReplayed(t *testing.T) {
	testdb.Each(t, func(t *testing.T, db *bun.DB) {
		s := bunstore.NewTOTPStore(db)
		cred := activeTOTP(t, s, "alice", "c1", 10)

		cred.LastUsedCounter = 9
		if err := s.SaveTOTP(t.Context(), cred); !errors.Is(err, totp.ErrTOTPReplayed) {
			t.Fatalf("SaveTOTP = %v, want ErrTOTPReplayed", err)
		}
	})
}

func TestSaveTOTPAtTheStoredCounterReturnsErrTOTPReplayed(t *testing.T) {
	testdb.Each(t, func(t *testing.T, db *bun.DB) {
		s := bunstore.NewTOTPStore(db)
		cred := activeTOTP(t, s, "alice", "c1", 10)

		if err := s.SaveTOTP(t.Context(), cred); !errors.Is(err, totp.ErrTOTPReplayed) {
			t.Fatalf("SaveTOTP = %v, want ErrTOTPReplayed", err)
		}
		got, err := s.GetActiveTOTP(t.Context(), "alice")
		if err != nil {
			t.Fatalf("GetActiveTOTP: %v", err)
		}
		if got.LastUsedCounter != 10 {
			t.Errorf("LastUsedCounter = %d, want 10", got.LastUsedCounter)
		}
	})
}

func TestSaveTOTPWritesOnlyTheCounter(t *testing.T) {
	testdb.Each(t, func(t *testing.T, db *bun.DB) {
		s := bunstore.NewTOTPStore(db)
		cred := activeTOTP(t, s, "alice", "c1", 10)

		cred.LastUsedCounter = 11
		cred.Secret = "changed"
		if err := s.SaveTOTP(t.Context(), cred); err != nil {
			t.Fatalf("SaveTOTP: %v", err)
		}
		got, err := s.GetActiveTOTP(t.Context(), "alice")
		if err != nil {
			t.Fatalf("GetActiveTOTP: %v", err)
		}
		if got.LastUsedCounter != 11 || got.Secret != "secret-c1" {
			t.Errorf("counter %d, secret %q; want 11, %q", got.LastUsedCounter, got.Secret, "secret-c1")
		}
	})
}

func TestTOTPCounterAboveMaxInt64(t *testing.T) {
	testdb.Each(t, func(t *testing.T, db *bun.DB) {
		ctx := t.Context()
		s := bunstore.NewTOTPStore(db)
		pending := &totp.Credential{ID: "p1", UserID: "alice", Secret: "s", CreatedAt: time.Now().UTC()}
		if err := s.EnrollPending(ctx, pending); err != nil {
			t.Fatalf("EnrollPending: %v", err)
		}

		if _, err := s.ConfirmEnrollment(ctx, "alice", "p1", uint64(math.MaxInt64)+1); err == nil {
			t.Fatal("ConfirmEnrollment above MaxInt64 returned nil")
		}
		if _, err := s.GetPendingTOTP(ctx, "alice"); err != nil {
			t.Errorf("GetPendingTOTP: %v, want the pending credential kept", err)
		}
		if _, err := s.GetActiveTOTP(ctx, "alice"); !errors.Is(err, totp.ErrTOTPNotEnrolled) {
			t.Errorf("GetActiveTOTP = %v, want ErrTOTPNotEnrolled", err)
		}

		active, err := s.ConfirmEnrollment(ctx, "alice", "p1", math.MaxInt64)
		if err != nil {
			t.Fatalf("ConfirmEnrollment at MaxInt64: %v", err)
		}
		if active.LastUsedCounter != math.MaxInt64 {
			t.Fatalf("LastUsedCounter = %d, want MaxInt64", active.LastUsedCounter)
		}

		active.LastUsedCounter = uint64(math.MaxInt64) + 1
		if err := s.SaveTOTP(ctx, active); err == nil {
			t.Fatal("SaveTOTP above MaxInt64 returned nil")
		}
		got, err := s.GetActiveTOTP(ctx, "alice")
		if err != nil {
			t.Fatalf("GetActiveTOTP: %v", err)
		}
		if got.LastUsedCounter != math.MaxInt64 {
			t.Errorf("stored counter = %d, want MaxInt64", got.LastUsedCounter)
		}
	})
}

func TestConfirmEnrollmentKeepsThePendingCreatedAt(t *testing.T) {
	testdb.Each(t, func(t *testing.T, db *bun.DB) {
		ctx := t.Context()
		s := bunstore.NewTOTPStore(db)
		t0 := time.Now().UTC().Truncate(time.Second)
		pending := &totp.Credential{ID: "p1", UserID: "alice", Secret: "s", CreatedAt: t0}
		if err := s.EnrollPending(ctx, pending); err != nil {
			t.Fatalf("EnrollPending: %v", err)
		}

		promoted, err := s.ConfirmEnrollment(ctx, "alice", "p1", 5)
		if err != nil {
			t.Fatalf("ConfirmEnrollment: %v", err)
		}
		if !promoted.CreatedAt.Equal(t0) {
			t.Errorf("promoted CreatedAt = %v, want %v", promoted.CreatedAt, t0)
		}
		got, err := s.GetActiveTOTP(ctx, "alice")
		if err != nil {
			t.Fatalf("GetActiveTOTP: %v", err)
		}
		if !got.CreatedAt.Equal(t0) {
			t.Errorf("stored CreatedAt = %v, want %v", got.CreatedAt, t0)
		}
	})
}

func TestTOTPStoreInsideCallerTransaction(t *testing.T) {
	testdb.Each(t, func(t *testing.T, db *bun.DB) {
		ctx := t.Context()
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatalf("BeginTx: %v", err)
		}
		defer func() { _ = tx.Rollback() }()

		cred := activeTOTP(t, bunstore.NewTOTPStore(tx), "alice", "c1", 10)
		cred.LastUsedCounter = 11
		if err := bunstore.NewTOTPStore(tx).SaveTOTP(ctx, cred); err != nil {
			t.Fatalf("SaveTOTP in tx: %v", err)
		}
		if _, err := bunstore.NewTOTPStore(tx).GetActiveTOTP(ctx, "alice"); err != nil {
			t.Fatalf("GetActiveTOTP in tx: %v", err)
		}
		if err := tx.Rollback(); err != nil {
			t.Fatalf("Rollback: %v", err)
		}

		if _, err := bunstore.NewTOTPStore(db).GetActiveTOTP(ctx, "alice"); !errors.Is(err, totp.ErrTOTPNotEnrolled) {
			t.Fatalf("GetActiveTOTP after rollback = %v, want ErrTOTPNotEnrolled", err)
		}
	})
}
