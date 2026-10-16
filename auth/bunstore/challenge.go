package bunstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/borfast/sulis/passkey"
	"github.com/uptrace/bun"
)

// ErrChallengeNotFound means no unexpired challenge exists under the key.
var ErrChallengeNotFound = errors.New("bunstore: challenge not found or expired")

const defaultChallengeTTL = 5 * time.Minute

type challengeModel struct {
	bun.BaseModel `bun:"table:passkey_challenges,alias:ch"`

	ChallengeKey string    `bun:"challenge_key,pk"`
	SessionData  []byte    `bun:"session_data"`
	ExpiresAt    time.Time `bun:"expires_at"`
}

// ChallengeStore implements passkey.ChallengeStore.
type ChallengeStore struct {
	db  bun.IDB
	ttl time.Duration
	now func() time.Time
}

var _ passkey.ChallengeStore = (*ChallengeStore)(nil)

// ChallengeOption configures a ChallengeStore.
type ChallengeOption func(*ChallengeStore)

// WithChallengeTTL sets how long a saved challenge stays valid. It panics
// if d is zero or negative.
func WithChallengeTTL(d time.Duration) ChallengeOption {
	if d <= 0 {
		panic("bunstore: challenge TTL must be positive")
	}

	return func(s *ChallengeStore) { s.ttl = d }
}

// NewChallengeStore returns a ChallengeStore with a 5 minute TTL by default.
func NewChallengeStore(db bun.IDB, opts ...ChallengeOption) *ChallengeStore {
	s := &ChallengeStore{db: db, ttl: defaultChallengeTTL, now: time.Now}
	for _, opt := range opts {
		opt(s)
	}

	return s
}

// SaveChallenge stores the data under key, replacing any earlier challenge.
func (s *ChallengeStore) SaveChallenge(ctx context.Context, key string, sessionData []byte) error {
	m := &challengeModel{
		ChallengeKey: key,
		SessionData:  sessionData,
		ExpiresAt:    s.now().UTC().Add(s.ttl),
	}
	_, err := s.db.NewInsert().
		Model(m).
		On("CONFLICT (challenge_key) DO UPDATE").
		Set("session_data = EXCLUDED.session_data").
		Set("expires_at = EXCLUDED.expires_at").
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("bunstore: saving challenge: %w", err)
	}

	return nil
}

// ConsumeChallenge deletes and returns the challenge in one statement, so
// only one caller can receive it. An expired challenge counts as missing.
func (s *ChallengeStore) ConsumeChallenge(ctx context.Context, key string) ([]byte, error) {
	m := new(challengeModel)
	err := s.db.NewDelete().
		Model(m).
		Where("challenge_key = ?", key).
		Returning("session_data, expires_at").
		Scan(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrChallengeNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("bunstore: consuming challenge: %w", err)
	}
	if !m.ExpiresAt.UTC().After(s.now()) {
		return nil, ErrChallengeNotFound
	}

	return m.SessionData, nil
}

// DeleteExpiredChallenges removes challenges past their expiry and returns
// how many it removed.
func (s *ChallengeStore) DeleteExpiredChallenges(ctx context.Context) (int, error) {
	res, err := s.db.NewDelete().
		Model((*challengeModel)(nil)).
		Where("expires_at <= ?", s.now().UTC()).
		Exec(ctx)
	if err != nil {
		return 0, fmt.Errorf("bunstore: deleting expired challenges: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("bunstore: counting deleted challenges: %w", err)
	}

	return int(n), nil
}
