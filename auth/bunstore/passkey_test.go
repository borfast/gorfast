package bunstore_test

import (
	"errors"
	"math"
	"slices"
	"testing"
	"time"

	"github.com/borfast/sulis/passkey"
	"github.com/borfast/sulis/storetest"
	"github.com/uptrace/bun"

	"github.com/borfast/gorfast/auth/bunstore"
	"github.com/borfast/gorfast/internal/testdb"
)

func TestPasskeyStore(t *testing.T) {
	testdb.Each(t, func(t *testing.T, db *bun.DB) {
		storetest.RunPasskeyStore(t, func() passkey.Store {
			testdb.Reset(t, db)
			return bunstore.NewPasskeyStore(db)
		})
	})
}

func newPasskey(userID, id string, createdAt time.Time) *passkey.Credential {
	return &passkey.Credential{
		ID:              id,
		UserID:          userID,
		CredentialID:    []byte("raw-" + id),
		PublicKey:       []byte("pub-" + id),
		AttestationType: "none",
		CreatedAt:       createdAt.UTC(),
	}
}

func TestSaveCredentialDuplicate(t *testing.T) {
	testdb.Each(t, func(t *testing.T, db *bun.DB) {
		s := bunstore.NewPasskeyStore(db)
		now := time.Now().UTC().Truncate(time.Second)
		a := newPasskey("alice", "a", now)
		if err := s.SaveCredential(t.Context(), a); err != nil {
			t.Fatalf("SaveCredential: %v", err)
		}

		sameRaw := newPasskey("alice", "other", now)
		sameRaw.CredentialID = a.CredentialID
		if err := s.SaveCredential(t.Context(), sameRaw); !errors.Is(err, bunstore.ErrPasskeyCredentialExists) {
			t.Errorf("same CredentialID: err = %v, want ErrPasskeyCredentialExists", err)
		}

		sameID := newPasskey("alice", "a", now)
		sameID.CredentialID = []byte("raw-different")
		if err := s.SaveCredential(t.Context(), sameID); !errors.Is(err, bunstore.ErrPasskeyCredentialExists) {
			t.Errorf("same ID: err = %v, want ErrPasskeyCredentialExists", err)
		}

		got, err := s.GetCredentialsByUserID(t.Context(), "alice")
		if err != nil {
			t.Fatalf("GetCredentialsByUserID: %v", err)
		}
		if len(got) != 1 {
			t.Errorf("got %d credentials, want 1", len(got))
		}
	})
}

func TestSaveCredentialDuplicateInsideCallerTransaction(t *testing.T) {
	testdb.Each(t, func(t *testing.T, db *bun.DB) {
		tx, err := db.BeginTx(t.Context(), nil)
		if err != nil {
			t.Fatalf("BeginTx: %v", err)
		}
		defer func() { _ = tx.Rollback() }()

		s := bunstore.NewPasskeyStore(tx)
		a := newPasskey("alice", "a", time.Now())
		if err := s.SaveCredential(t.Context(), a); err != nil {
			t.Fatalf("SaveCredential: %v", err)
		}
		if err := s.SaveCredential(t.Context(), a); !errors.Is(err, bunstore.ErrPasskeyCredentialExists) {
			t.Fatalf("duplicate: err = %v, want ErrPasskeyCredentialExists", err)
		}

		got, err := s.GetCredentialsByUserID(t.Context(), "alice")
		if err != nil {
			t.Fatalf("GetCredentialsByUserID after duplicate: %v", err)
		}
		if len(got) != 1 {
			t.Errorf("got %d credentials, want 1", len(got))
		}
	})
}

func TestPasskeySignCountAtMaxUint32(t *testing.T) {
	testdb.Each(t, func(t *testing.T, db *bun.DB) {
		s := bunstore.NewPasskeyStore(db)
		a := newPasskey("alice", "a", time.Now())
		a.SignCount = math.MaxUint32 - 1
		if err := s.SaveCredential(t.Context(), a); err != nil {
			t.Fatalf("SaveCredential: %v", err)
		}
		got, err := s.GetCredentialByID(t.Context(), a.CredentialID)
		if err != nil {
			t.Fatalf("GetCredentialByID: %v", err)
		}
		if got.SignCount != math.MaxUint32-1 {
			t.Fatalf("SignCount = %d, want %d", got.SignCount, uint32(math.MaxUint32-1))
		}

		err = s.UpdateCredentialAfterLogin(t.Context(), a.CredentialID, math.MaxUint32, true, time.Now())
		if err != nil {
			t.Fatalf("UpdateCredentialAfterLogin: %v", err)
		}
		got, err = s.GetCredentialByID(t.Context(), a.CredentialID)
		if err != nil {
			t.Fatalf("GetCredentialByID: %v", err)
		}
		if got.SignCount != math.MaxUint32 {
			t.Errorf("SignCount = %d, want %d", got.SignCount, uint32(math.MaxUint32))
		}
	})
}

func TestGetCredentialsByUserIDOrder(t *testing.T) {
	testdb.Each(t, func(t *testing.T, db *bun.DB) {
		s := bunstore.NewPasskeyStore(db)
		t0 := time.Now().UTC().Truncate(time.Second)
		for _, c := range []*passkey.Credential{
			newPasskey("alice", "b", t0),
			newPasskey("alice", "a", t0),
			newPasskey("alice", "c", t0.Add(-time.Hour)),
		} {
			if err := s.SaveCredential(t.Context(), c); err != nil {
				t.Fatalf("SaveCredential: %v", err)
			}
		}

		got, err := s.GetCredentialsByUserID(t.Context(), "alice")
		if err != nil {
			t.Fatalf("GetCredentialsByUserID: %v", err)
		}
		var ids []string
		for _, c := range got {
			ids = append(ids, c.ID)
		}
		if want := []string{"c", "a", "b"}; !slices.Equal(ids, want) {
			t.Errorf("IDs = %v, want %v", ids, want)
		}
	})
}

func TestPasskeyNotFoundIsErrPasskeyNotFound(t *testing.T) {
	testdb.Each(t, func(t *testing.T, db *bun.DB) {
		s := bunstore.NewPasskeyStore(db)

		if _, err := s.GetCredentialByID(t.Context(), []byte("nope")); !errors.Is(err, passkey.ErrPasskeyNotFound) {
			t.Errorf("GetCredentialByID: err = %v, want ErrPasskeyNotFound", err)
		}
		err := s.UpdateCredentialAfterLogin(t.Context(), []byte("nope"), 1, false, time.Now())
		if !errors.Is(err, passkey.ErrPasskeyNotFound) {
			t.Errorf("UpdateCredentialAfterLogin: err = %v, want ErrPasskeyNotFound", err)
		}
	})
}

func TestPasskeyStoreInsideCallerTransaction(t *testing.T) {
	testdb.Each(t, func(t *testing.T, db *bun.DB) {
		tx, err := db.BeginTx(t.Context(), nil)
		if err != nil {
			t.Fatalf("BeginTx: %v", err)
		}
		defer func() { _ = tx.Rollback() }()

		s := bunstore.NewPasskeyStore(tx)
		now := time.Now()
		for _, id := range []string{"a", "b"} {
			if err := s.SaveCredential(t.Context(), newPasskey("alice", id, now)); err != nil {
				t.Fatalf("SaveCredential: %v", err)
			}
		}
		if err := s.DeleteCredential(t.Context(), "alice", "a", false); err != nil {
			t.Fatalf("DeleteCredential: %v", err)
		}
		got, err := s.GetCredentialsByUserID(t.Context(), "alice")
		if err != nil || len(got) != 1 {
			t.Fatalf("inside tx: %d credentials, err %v, want 1", len(got), err)
		}

		if err := tx.Rollback(); err != nil {
			t.Fatalf("Rollback: %v", err)
		}
		after, err := bunstore.NewPasskeyStore(db).GetCredentialsByUserID(t.Context(), "alice")
		if err != nil || len(after) != 0 {
			t.Errorf("after rollback: %d credentials, err %v, want 0", len(after), err)
		}
	})
}
