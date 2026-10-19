package bunstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
	"uuid"

	"github.com/borfast/sulis"
	"github.com/uptrace/bun"
)

type sessionModel struct {
	bun.BaseModel `bun:"table:sessions,alias:s"`

	ID              ID             `bun:"id,pk"`
	UserID          ID             `bun:"user_id"`
	TokenHash       string         `bun:"token_hash"`
	ExpiresAt       time.Time      `bun:"expires_at"`
	CreatedAt       time.Time      `bun:"created_at"`
	AuthenticatedAt time.Time      `bun:"authenticated_at"`
	Method          string         `bun:"method"`
	LastSeenAt      time.Time      `bun:"last_seen_at"`
	IdleExpiresAt   *time.Time     `bun:"idle_expires_at"`
	IP              string         `bun:"ip"`
	UserAgent       string         `bun:"user_agent"`
	Metadata        map[string]any `bun:"metadata"`
}

func toSessionModel(s *sulis.Session) *sessionModel {
	return &sessionModel{
		ID:              ID(s.ID),
		UserID:          ID(s.UserID),
		TokenHash:       s.TokenHash,
		ExpiresAt:       s.ExpiresAt,
		CreatedAt:       s.CreatedAt,
		AuthenticatedAt: s.AuthenticatedAt,
		Method:          string(s.Method),
		LastSeenAt:      s.LastSeenAt,
		IdleExpiresAt:   s.IdleExpiresAt,
		IP:              s.IP,
		UserAgent:       s.UserAgent,
		Metadata:        s.Metadata,
	}
}

func fromSessionModel(m *sessionModel) *sulis.Session {
	return &sulis.Session{
		ID:              uuid.UUID(m.ID),
		UserID:          uuid.UUID(m.UserID),
		TokenHash:       m.TokenHash,
		ExpiresAt:       m.ExpiresAt.UTC(),
		CreatedAt:       m.CreatedAt.UTC(),
		AuthenticatedAt: m.AuthenticatedAt.UTC(),
		Method:          sulis.AuthMethod(m.Method),
		LastSeenAt:      m.LastSeenAt.UTC(),
		IdleExpiresAt:   utcPtr(m.IdleExpiresAt),
		IP:              m.IP,
		UserAgent:       m.UserAgent,
		Metadata:        m.Metadata,
	}
}

// SessionStore implements sulis.SessionStore.
type SessionStore struct {
	db bun.IDB
}

var _ sulis.SessionStore = (*SessionStore)(nil)

// NewSessionStore returns a SessionStore reading and writing through db.
func NewSessionStore(db bun.IDB) *SessionStore {
	return &SessionStore{db: db}
}

func (s *SessionStore) CreateSession(ctx context.Context, session *sulis.Session) error {
	_, err := s.db.NewInsert().Model(toSessionModel(session)).Exec(ctx)
	if err != nil {
		return fmt.Errorf("bunstore: creating session: %w", err)
	}

	return nil
}

func (s *SessionStore) GetSessionByTokenHash(ctx context.Context, tokenHash string) (*sulis.Session, error) {
	m := new(sessionModel)
	err := s.db.NewSelect().Model(m).Where("token_hash = ?", tokenHash).Scan(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, sulis.ErrSessionNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("bunstore: reading session: %w", err)
	}

	return fromSessionModel(m), nil
}

// ListUserSessions returns every session for userID. Matching nothing is not
// an error, and TokenHash is returned exactly as stored.
func (s *SessionStore) ListUserSessions(ctx context.Context, userID uuid.UUID) ([]sulis.Session, error) {
	var models []sessionModel
	err := s.db.NewSelect().Model(&models).Where("user_id = ?", ID(userID)).Scan(ctx)
	if err != nil {
		return nil, fmt.Errorf("bunstore: listing sessions: %w", err)
	}

	sessions := make([]sulis.Session, 0, len(models))
	for i := range models {
		sessions = append(sessions, *fromSessionModel(&models[i]))
	}

	return sessions, nil
}

// DeleteSession scopes the delete to both columns, so a session ID belonging
// to another user matches no row rather than being checked afterwards.
func (s *SessionStore) DeleteSession(ctx context.Context, userID, id uuid.UUID) error {
	res, err := s.db.NewDelete().
		Model((*sessionModel)(nil)).
		Where("id = ?", ID(id)).
		Where("user_id = ?", ID(userID)).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("bunstore: deleting session: %w", err)
	}

	return requireOneRow(res, sulis.ErrSessionNotFound, "deleting session")
}

func (s *SessionStore) DeleteUserSessions(ctx context.Context, userID uuid.UUID) error {
	_, err := s.db.NewDelete().Model((*sessionModel)(nil)).Where("user_id = ?", ID(userID)).Exec(ctx)
	if err != nil {
		return fmt.Errorf("bunstore: deleting user sessions: %w", err)
	}

	return nil
}

// DeleteUserSessionsExcept is the "sign out everywhere else" primitive. A
// keepSessionID that does not exist is not an error.
func (s *SessionStore) DeleteUserSessionsExcept(ctx context.Context, userID, keepSessionID uuid.UUID) error {
	_, err := s.db.NewDelete().
		Model((*sessionModel)(nil)).
		Where("user_id = ?", ID(userID)).
		Where("id <> ?", ID(keepSessionID)).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("bunstore: deleting other user sessions: %w", err)
	}

	return nil
}

func (s *SessionStore) CleanExpired(ctx context.Context) error {
	_, err := s.db.NewDelete().
		Model((*sessionModel)(nil)).
		Where("expires_at < ?", time.Now().UTC()).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("bunstore: cleaning expired sessions: %w", err)
	}

	return nil
}

// UpdateAuthenticatedAt stamps the session and touches nothing else. It is
// the write path behind ReAuthenticate.
func (s *SessionStore) UpdateAuthenticatedAt(ctx context.Context, id uuid.UUID, at time.Time) error {
	res, err := s.db.NewUpdate().
		Model((*sessionModel)(nil)).
		Set("authenticated_at = ?", at).
		Where("id = ?", ID(id)).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("bunstore: stamping authenticated_at: %w", err)
	}

	return requireOneRow(res, sulis.ErrSessionNotFound, "stamping authenticated_at")
}

func (s *SessionStore) TouchSession(ctx context.Context, id uuid.UUID, lastSeen time.Time, idleExpires *time.Time) error {
	res, err := s.db.NewUpdate().
		Model((*sessionModel)(nil)).
		Set("last_seen_at = ?", lastSeen).
		Set("idle_expires_at = ?", idleExpires).
		Where("id = ?", ID(id)).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("bunstore: touching session: %w", err)
	}

	return requireOneRow(res, sulis.ErrSessionNotFound, "touching session")
}
