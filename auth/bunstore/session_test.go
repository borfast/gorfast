package bunstore_test

import (
	"testing"
	"time"
	"uuid"

	"github.com/borfast/sulis"
	"github.com/borfast/sulis/storetest"
	"github.com/uptrace/bun"

	"github.com/borfast/gorfast/auth/bunstore"
	"github.com/borfast/gorfast/internal/testdb"
)

func TestSessionStore(t *testing.T) {
	testdb.Each(t, func(t *testing.T, db *bun.DB) {
		storetest.RunSessionStore(t, func() sulis.SessionStore {
			testdb.Reset(t, db)
			return bunstore.NewSessionStore(db)
		})
	})
}

// TestDeleteUserSessionsExceptNilDeletesEverySession pins the contract that a
// keepSessionID naming no session, such as uuid.Nil(), keeps nothing.
func TestDeleteUserSessionsExceptNilDeletesEverySession(t *testing.T) {
	testdb.Each(t, func(t *testing.T, db *bun.DB) {
		ctx := t.Context()
		s := bunstore.NewSessionStore(db)
		userID := uuid.NewV7()
		now := time.Now().UTC()
		for i := range 2 {
			session := &sulis.Session{
				ID: uuid.NewV7(), UserID: userID, TokenHash: uuid.NewV7().String(),
				ExpiresAt: now.Add(time.Hour), CreatedAt: now, AuthenticatedAt: now, LastSeenAt: now,
			}
			if err := s.CreateSession(ctx, session); err != nil {
				t.Fatalf("CreateSession %d: %v", i, err)
			}
		}

		if err := s.DeleteUserSessionsExcept(ctx, userID, uuid.Nil()); err != nil {
			t.Fatalf("DeleteUserSessionsExcept: %v", err)
		}
		got, err := s.ListUserSessions(ctx, userID)
		if err != nil {
			t.Fatalf("ListUserSessions: %v", err)
		}
		if len(got) != 0 {
			t.Errorf("%d sessions left, want 0", len(got))
		}
	})
}
