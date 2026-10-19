package bunstore_test

import (
	"errors"
	"math"
	"slices"
	"testing"
	"time"
	"uuid"

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

func newPasskey(userID, id uuid.UUID, createdAt time.Time) *passkey.Credential {
	return &passkey.Credential{
		ID:              id,
		UserID:          userID,
		CredentialID:    []byte("raw-" + id.String()),
		PublicKey:       []byte("pub-" + id.String()),
		AttestationType: "none",
		CreatedAt:       createdAt.UTC(),
	}
}

func TestSaveCredentialDuplicate(t *testing.T) {
	testdb.Each(t, func(t *testing.T, db *bun.DB) {
		s := bunstore.NewPasskeyStore(db)
		now := time.Now().UTC().Truncate(time.Second)
		a := newPasskey(testID("alice"), testID("a"), now)
		if err := s.SaveCredential(t.Context(), a); err != nil {
			t.Fatalf("SaveCredential: %v", err)
		}

		sameRaw := newPasskey(testID("alice"), testID("other"), now)
		sameRaw.CredentialID = a.CredentialID
		if err := s.SaveCredential(t.Context(), sameRaw); !errors.Is(err, bunstore.ErrPasskeyCredentialExists) {
			t.Errorf("same CredentialID: err = %v, want ErrPasskeyCredentialExists", err)
		}

		sameID := newPasskey(testID("alice"), testID("a"), now)
		sameID.CredentialID = []byte("raw-different")
		if err := s.SaveCredential(t.Context(), sameID); !errors.Is(err, bunstore.ErrPasskeyCredentialExists) {
			t.Errorf("same ID: err = %v, want ErrPasskeyCredentialExists", err)
		}

		got, err := s.GetCredentialsByUserID(t.Context(), testID("alice"))
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
		a := newPasskey(testID("alice"), testID("a"), time.Now())
		if err := s.SaveCredential(t.Context(), a); err != nil {
			t.Fatalf("SaveCredential: %v", err)
		}
		if err := s.SaveCredential(t.Context(), a); !errors.Is(err, bunstore.ErrPasskeyCredentialExists) {
			t.Fatalf("duplicate: err = %v, want ErrPasskeyCredentialExists", err)
		}

		got, err := s.GetCredentialsByUserID(t.Context(), testID("alice"))
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
		a := newPasskey(testID("alice"), testID("a"), time.Now())
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

		var raw int64
		err = db.NewRaw("SELECT sign_count FROM passkey_credentials WHERE id = ?", bunstore.ID(a.ID)).Scan(t.Context(), &raw)
		if err != nil {
			t.Fatalf("reading sign_count: %v", err)
		}
		if raw != math.MaxUint32 {
			t.Errorf("sign_count column = %d, want %d", raw, int64(math.MaxUint32))
		}
	})
}

func TestPasskeyNilFieldsReadBackAsNil(t *testing.T) {
	testdb.Each(t, func(t *testing.T, db *bun.DB) {
		s := bunstore.NewPasskeyStore(db)
		a := newPasskey(testID("alice"), testID("a"), time.Now())
		if err := s.SaveCredential(t.Context(), a); err != nil {
			t.Fatalf("SaveCredential: %v", err)
		}
		got, err := s.GetCredentialByID(t.Context(), a.CredentialID)
		if err != nil {
			t.Fatalf("GetCredentialByID: %v", err)
		}
		if got.AAGUID != nil {
			t.Errorf("AAGUID = %v, want nil", got.AAGUID)
		}
		if got.Transports != nil {
			t.Errorf("Transports = %v, want nil", got.Transports)
		}
		if got.LastUsedAt != nil {
			t.Errorf("LastUsedAt = %v, want nil", got.LastUsedAt)
		}
	})
}

func TestGetCredentialsByUserIDOrder(t *testing.T) {
	testdb.Each(t, func(t *testing.T, db *bun.DB) {
		s := bunstore.NewPasskeyStore(db)
		t0 := time.Now().UTC().Truncate(time.Second)
		// NewV7 returns increasing IDs, so a < b < c.
		a, b, c := uuid.NewV7(), uuid.NewV7(), uuid.NewV7()
		for _, cred := range []*passkey.Credential{
			newPasskey(testID("alice"), b, t0),
			newPasskey(testID("alice"), a, t0),
			newPasskey(testID("alice"), c, t0.Add(-time.Hour)),
		} {
			if err := s.SaveCredential(t.Context(), cred); err != nil {
				t.Fatalf("SaveCredential: %v", err)
			}
		}

		got, err := s.GetCredentialsByUserID(t.Context(), testID("alice"))
		if err != nil {
			t.Fatalf("GetCredentialsByUserID: %v", err)
		}
		var ids []uuid.UUID
		for _, cred := range got {
			ids = append(ids, cred.ID)
		}
		if want := []uuid.UUID{c, a, b}; !slices.Equal(ids, want) {
			t.Errorf("IDs = %v, want %v", ids, want)
		}
	})
}

func TestPasskeyNotFoundIsErrPasskeyNotFound(t *testing.T) {
	testdb.Each(t, func(t *testing.T, db *bun.DB) {
		s := bunstore.NewPasskeyStore(db)

		if _, err := s.GetCredentialByID(t.Context(), []byte("nope")); err != passkey.ErrPasskeyNotFound {
			t.Errorf("GetCredentialByID: err = %v, want ErrPasskeyNotFound", err)
		}
		err := s.UpdateCredentialAfterLogin(t.Context(), []byte("nope"), 1, false, time.Now())
		if err != passkey.ErrPasskeyNotFound {
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
		for _, id := range []uuid.UUID{testID("a"), testID("b")} {
			if err := s.SaveCredential(t.Context(), newPasskey(testID("alice"), id, now)); err != nil {
				t.Fatalf("SaveCredential: %v", err)
			}
		}
		if err := s.DeleteCredential(t.Context(), testID("alice"), testID("a"), false); err != nil {
			t.Fatalf("DeleteCredential: %v", err)
		}
		got, err := s.GetCredentialsByUserID(t.Context(), testID("alice"))
		if err != nil || len(got) != 1 {
			t.Fatalf("inside tx: %d credentials, err %v, want 1", len(got), err)
		}

		if err := tx.Rollback(); err != nil {
			t.Fatalf("Rollback: %v", err)
		}
		after, err := bunstore.NewPasskeyStore(db).GetCredentialsByUserID(t.Context(), testID("alice"))
		if err != nil || len(after) != 0 {
			t.Errorf("after rollback: %d credentials, err %v, want 0", len(after), err)
		}
	})
}
