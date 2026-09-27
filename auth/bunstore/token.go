package bunstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/borfast/sulis"
	"github.com/uptrace/bun"
)

type tokenModel struct {
	bun.BaseModel `bun:"table:tokens,alias:t"`

	ID        string    `bun:"id,pk"`
	UserID    string    `bun:"user_id"`
	TokenHash string    `bun:"token_hash"`
	Purpose   string    `bun:"purpose"`
	ExpiresAt time.Time `bun:"expires_at"`
	CreatedAt time.Time `bun:"created_at"`
	Used      bool      `bun:"used"`
	Email     string    `bun:"email"`
	NonceHash string    `bun:"nonce_hash"`
}

func toTokenModel(t *sulis.Token) *tokenModel {
	return &tokenModel{
		ID:        t.ID,
		UserID:    t.UserID,
		TokenHash: t.TokenHash,
		Purpose:   string(t.Purpose),
		ExpiresAt: t.ExpiresAt,
		CreatedAt: t.CreatedAt,
		Used:      t.Used,
		Email:     t.Email,
		NonceHash: t.NonceHash,
	}
}

func fromTokenModel(m *tokenModel) *sulis.Token {
	return &sulis.Token{
		ID:        m.ID,
		UserID:    m.UserID,
		TokenHash: m.TokenHash,
		Purpose:   sulis.TokenPurpose(m.Purpose),
		ExpiresAt: m.ExpiresAt.UTC(),
		CreatedAt: m.CreatedAt.UTC(),
		Used:      m.Used,
		Email:     m.Email,
		NonceHash: m.NonceHash,
	}
}

// TokenStore implements sulis.TokenStore.
type TokenStore struct {
	db bun.IDB
}

var _ sulis.TokenStore = (*TokenStore)(nil)

// NewTokenStore returns a TokenStore reading and writing through db.
func NewTokenStore(db bun.IDB) *TokenStore {
	return &TokenStore{db: db}
}

func (s *TokenStore) CreateToken(ctx context.Context, token *sulis.Token) error {
	_, err := s.db.NewInsert().Model(toTokenModel(token)).Exec(ctx)
	if err != nil {
		return fmt.Errorf("bunstore: creating token: %w", err)
	}

	return nil
}

// ConsumeToken finds and marks the token in one statement, so two concurrent
// presentations of the same token cannot both succeed.
func (s *TokenStore) ConsumeToken(ctx context.Context, hash string, purpose sulis.TokenPurpose) (*sulis.Token, error) {
	m := new(tokenModel)
	err := s.db.NewUpdate().
		Model(m).
		Set("used = ?", true).
		Where("token_hash = ?", hash).
		Where("purpose = ?", string(purpose)).
		Where("used = ?", false).
		Returning("*").
		Scan(ctx)
	if err == nil {
		m.Used = true
		return fromTokenModel(m), nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("bunstore: consuming token: %w", err)
	}

	return nil, s.explainFailedConsume(ctx, hash, purpose)
}

// explainFailedConsume distinguishes a token that does not exist from one
// already spent. The UPDATE alone cannot, and the contract names an error for
// each. A token never goes back to unused, so this follow-up read cannot be
// wrong about which case it is.
func (s *TokenStore) explainFailedConsume(ctx context.Context, hash string, purpose sulis.TokenPurpose) error {
	exists, err := s.db.NewSelect().
		Model((*tokenModel)(nil)).
		Where("token_hash = ?", hash).
		Where("purpose = ?", string(purpose)).
		Exists(ctx)
	if err != nil {
		return fmt.Errorf("bunstore: consuming token: %w", err)
	}
	if !exists {
		return sulis.ErrTokenNotFound
	}

	return sulis.ErrTokenAlreadyUsed
}

func (s *TokenStore) DeleteExpiredTokens(ctx context.Context) error {
	_, err := s.db.NewDelete().
		Model((*tokenModel)(nil)).
		Where("expires_at < ?", time.Now().UTC()).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("bunstore: deleting expired tokens: %w", err)
	}

	return nil
}

// DeleteUserTokens removes every token for the user and purpose. Deleting
// zero tokens is not an error.
func (s *TokenStore) DeleteUserTokens(ctx context.Context, userID string, purpose sulis.TokenPurpose) error {
	_, err := s.db.NewDelete().
		Model((*tokenModel)(nil)).
		Where("user_id = ?", userID).
		Where("purpose = ?", string(purpose)).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("bunstore: deleting user tokens: %w", err)
	}

	return nil
}
