package bunstore

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/borfast/sulis/recovery"
	"github.com/uptrace/bun"
)

// recoverySetModel is the per-user lock row for recovery codes.
type recoverySetModel struct {
	bun.BaseModel `bun:"table:recovery_code_sets,alias:rs"`

	UserID     string    `bun:"user_id,pk"`
	ReplacedAt time.Time `bun:"replaced_at"`
}

type recoveryCodeModel struct {
	bun.BaseModel `bun:"table:recovery_codes,alias:rc"`

	UserID   string `bun:"user_id,pk"`
	CodeHash string `bun:"code_hash,pk"`
}

// RecoveryStore implements recovery.Store. Every write starts by writing the
// user's recovery_code_sets row, so writes for one user run one at a time.
type RecoveryStore struct {
	db bun.IDB
}

var _ recovery.Store = (*RecoveryStore)(nil)

// NewRecoveryStore returns a RecoveryStore reading and writing through db.
func NewRecoveryStore(db bun.IDB) *RecoveryStore {
	return &RecoveryStore{db: db}
}

func (s *RecoveryStore) ReplaceCodes(ctx context.Context, userID string, hashes []string) error {
	err := s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		set := &recoverySetModel{UserID: userID, ReplacedAt: time.Now().UTC()}
		_, err := tx.NewInsert().
			Model(set).
			On("CONFLICT (user_id) DO UPDATE").
			Set("replaced_at = EXCLUDED.replaced_at").
			Exec(ctx)
		if err != nil {
			return err
		}

		_, err = tx.NewDelete().Model((*recoveryCodeModel)(nil)).Where("user_id = ?", userID).Exec(ctx)
		if err != nil {
			return err
		}

		seen := make(map[string]struct{}, len(hashes))
		codes := make([]recoveryCodeModel, 0, len(hashes))
		for _, h := range hashes {
			if _, dup := seen[h]; dup {
				continue
			}
			seen[h] = struct{}{}
			codes = append(codes, recoveryCodeModel{UserID: userID, CodeHash: h})
		}
		if len(codes) == 0 {
			return nil
		}

		_, err = tx.NewInsert().Model(&codes).Exec(ctx)
		return err
	})
	if err != nil {
		return fmt.Errorf("bunstore: replacing recovery codes: %w", err)
	}

	return nil
}

func (s *RecoveryStore) ConsumeCode(ctx context.Context, userID, hash string) (int, error) {
	remaining := 0
	err := s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		res, err := tx.NewUpdate().
			Model((*recoverySetModel)(nil)).
			Set("user_id = user_id").
			Where("user_id = ?", userID).
			Exec(ctx)
		if err != nil {
			return err
		}
		if err := requireOneRow(res, recovery.ErrCodeNotFound, "consuming recovery code"); err != nil {
			return err
		}

		res, err = tx.NewDelete().
			Model((*recoveryCodeModel)(nil)).
			Where("user_id = ?", userID).
			Where("code_hash = ?", hash).
			Exec(ctx)
		if err != nil {
			return err
		}
		if err := requireOneRow(res, recovery.ErrCodeNotFound, "consuming recovery code"); err != nil {
			return err
		}

		remaining, err = tx.NewSelect().Model((*recoveryCodeModel)(nil)).Where("user_id = ?", userID).Count(ctx)
		return err
	})
	if errors.Is(err, recovery.ErrCodeNotFound) {
		return 0, recovery.ErrCodeNotFound
	}
	if err != nil {
		return 0, fmt.Errorf("bunstore: consuming recovery code: %w", err)
	}

	return remaining, nil
}

// CountCodes takes no lock.
func (s *RecoveryStore) CountCodes(ctx context.Context, userID string) (int, error) {
	n, err := s.db.NewSelect().Model((*recoveryCodeModel)(nil)).Where("user_id = ?", userID).Count(ctx)
	if err != nil {
		return 0, fmt.Errorf("bunstore: counting recovery codes: %w", err)
	}

	return n, nil
}

func (s *RecoveryStore) DeleteCodes(ctx context.Context, userID string) error {
	err := s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		_, err := tx.NewDelete().Model((*recoverySetModel)(nil)).Where("user_id = ?", userID).Exec(ctx)
		if err != nil {
			return err
		}
		_, err = tx.NewDelete().Model((*recoveryCodeModel)(nil)).Where("user_id = ?", userID).Exec(ctx)
		return err
	})
	if err != nil {
		return fmt.Errorf("bunstore: deleting recovery codes: %w", err)
	}

	return nil
}
