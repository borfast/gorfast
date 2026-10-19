package bunstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"time"
	"uuid"

	"github.com/borfast/sulis/totp"
	"github.com/uptrace/bun"
)

// totpModel holds both slots of a user's TOTP enrollment. A nil ID means the
// slot is empty.
type totpModel struct {
	bun.BaseModel `bun:"table:totp_credentials,alias:tc"`

	UserID           ID         `bun:"user_id,pk"`
	ActiveID         *ID        `bun:"active_id"`
	ActiveSecret     *string    `bun:"active_secret"`
	ActiveCounter    *int64     `bun:"active_counter"`
	ActiveCreatedAt  *time.Time `bun:"active_created_at"`
	PendingID        *ID        `bun:"pending_id"`
	PendingSecret    *string    `bun:"pending_secret"`
	PendingCreatedAt *time.Time `bun:"pending_created_at"`
}

func toPendingTOTPModel(c *totp.Credential) *totpModel {
	created := c.CreatedAt.UTC()
	pendingID := ID(c.ID)
	return &totpModel{
		UserID:           ID(c.UserID),
		PendingID:        &pendingID,
		PendingSecret:    &c.Secret,
		PendingCreatedAt: &created,
	}
}

// fromActiveTOTPModel returns nil when the active slot is empty.
func fromActiveTOTPModel(m *totpModel) *totp.Credential {
	if m.ActiveID == nil {
		return nil
	}
	c := &totp.Credential{ID: uuid.UUID(*m.ActiveID), UserID: uuid.UUID(m.UserID), Verified: true}
	if m.ActiveSecret != nil {
		c.Secret = *m.ActiveSecret
	}
	if m.ActiveCounter != nil {
		c.LastUsedCounter = uint64(*m.ActiveCounter)
	}
	if t := utcPtr(m.ActiveCreatedAt); t != nil {
		c.CreatedAt = *t
	}

	return c
}

// fromPendingTOTPModel returns nil when the pending slot is empty.
func fromPendingTOTPModel(m *totpModel) *totp.Credential {
	if m.PendingID == nil {
		return nil
	}
	c := &totp.Credential{ID: uuid.UUID(*m.PendingID), UserID: uuid.UUID(m.UserID)}
	if m.PendingSecret != nil {
		c.Secret = *m.PendingSecret
	}
	if t := utcPtr(m.PendingCreatedAt); t != nil {
		c.CreatedAt = *t
	}

	return c
}

// checkTOTPCounter rejects a counter that BIGINT cannot hold, before any SQL.
func checkTOTPCounter(counter uint64) error {
	if counter > math.MaxInt64 {
		return fmt.Errorf("bunstore: TOTP counter %d does not fit in BIGINT", counter)
	}

	return nil
}

// TOTPStore implements totp.Store.
type TOTPStore struct {
	db bun.IDB
}

var _ totp.Store = (*TOTPStore)(nil)

// NewTOTPStore returns a TOTPStore reading and writing through db.
func NewTOTPStore(db bun.IDB) *TOTPStore {
	return &TOTPStore{db: db}
}

func (s *TOTPStore) GetActiveTOTP(ctx context.Context, userID uuid.UUID) (*totp.Credential, error) {
	m := new(totpModel)
	err := s.db.NewSelect().Model(m).Where("user_id = ?", ID(userID)).Where("active_id IS NOT NULL").Scan(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, totp.ErrTOTPNotEnrolled
	}
	if err != nil {
		return nil, fmt.Errorf("bunstore: getting active TOTP: %w", err)
	}

	return fromActiveTOTPModel(m), nil
}

func (s *TOTPStore) GetPendingTOTP(ctx context.Context, userID uuid.UUID) (*totp.Credential, error) {
	m := new(totpModel)
	err := s.db.NewSelect().Model(m).Where("user_id = ?", ID(userID)).Where("pending_id IS NOT NULL").Scan(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, totp.ErrTOTPNotEnrolled
	}
	if err != nil {
		return nil, fmt.Errorf("bunstore: getting pending TOTP: %w", err)
	}

	return fromPendingTOTPModel(m), nil
}

// EnrollPending writes the pending slot only while the active slot is empty.
// The guard is part of the upsert, so the check and the write are one step.
func (s *TOTPStore) EnrollPending(ctx context.Context, cred *totp.Credential) error {
	res, err := s.upsertPending(cred).Where("?TableAlias.active_id IS NULL").Exec(ctx)
	if err != nil {
		return fmt.Errorf("bunstore: enrolling pending TOTP: %w", err)
	}

	return requireOneRow(res, totp.ErrTOTPAlreadyEnrolled, "enrolling pending TOTP")
}

// ReplacePending writes the pending slot and leaves the active slot alone.
func (s *TOTPStore) ReplacePending(ctx context.Context, cred *totp.Credential) error {
	if _, err := s.upsertPending(cred).Exec(ctx); err != nil {
		return fmt.Errorf("bunstore: replacing pending TOTP: %w", err)
	}

	return nil
}

// upsertPending's DO UPDATE sets only the pending columns.
func (s *TOTPStore) upsertPending(cred *totp.Credential) *bun.InsertQuery {
	return s.db.NewInsert().
		Model(toPendingTOTPModel(cred)).
		Column("user_id", "pending_id", "pending_secret", "pending_created_at").
		On("CONFLICT (user_id) DO UPDATE").
		Set("pending_id = EXCLUDED.pending_id").
		Set("pending_secret = EXCLUDED.pending_secret").
		Set("pending_created_at = EXCLUDED.pending_created_at")
}

func (s *TOTPStore) ConfirmEnrollment(ctx context.Context, userID, pendingID uuid.UUID, counter uint64) (*totp.Credential, error) {
	if err := checkTOTPCounter(counter); err != nil {
		return nil, err
	}

	m := new(totpModel)
	c := int64(counter)
	err := s.db.NewUpdate().
		Model(m).
		Set("active_id = pending_id").
		Set("active_secret = pending_secret").
		Set("active_created_at = pending_created_at").
		Set("active_counter = CASE WHEN active_counter IS NULL OR active_counter < ? THEN ? ELSE active_counter END", c, c).
		Set("pending_id = NULL").
		Set("pending_secret = NULL").
		Set("pending_created_at = NULL").
		Where("user_id = ?", ID(userID)).
		Where("pending_id = ?", ID(pendingID)).
		Returning("user_id, active_id, active_secret, active_counter, active_created_at").
		Scan(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, totp.ErrTOTPNotEnrolled
	}
	if err != nil {
		return nil, fmt.Errorf("bunstore: confirming TOTP enrollment: %w", err)
	}

	return fromActiveTOTPModel(m), nil
}

// SaveTOTP raises only the active credential's counter. A refused save returns
// totp.ErrTOTPReplayed, or totp.ErrTOTPNotEnrolled if cred.ID is not active.
func (s *TOTPStore) SaveTOTP(ctx context.Context, cred *totp.Credential) error {
	if err := checkTOTPCounter(cred.LastUsedCounter); err != nil {
		return err
	}

	return s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		res, err := tx.NewUpdate().
			Model((*totpModel)(nil)).
			Set("active_counter = ?", int64(cred.LastUsedCounter)).
			Where("user_id = ?", ID(cred.UserID)).
			Where("active_id = ?", ID(cred.ID)).
			Where("active_counter < ?", int64(cred.LastUsedCounter)).
			Exec(ctx)
		if err != nil {
			return fmt.Errorf("bunstore: saving TOTP: %w", err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return fmt.Errorf("bunstore: saving TOTP: %w", err)
		}
		if n > 0 {
			return nil
		}

		return explainFailedSave(ctx, tx, cred)
	})
}

// explainFailedSave tells a counter that did not go up apart from a credential
// that is gone or replaced.
func explainFailedSave(ctx context.Context, db bun.IDB, cred *totp.Credential) error {
	var activeID *ID
	err := db.NewSelect().
		Model((*totpModel)(nil)).
		Column("active_id").
		Where("user_id = ?", ID(cred.UserID)).
		Scan(ctx, &activeID)
	if errors.Is(err, sql.ErrNoRows) {
		return totp.ErrTOTPNotEnrolled
	}
	if err != nil {
		return fmt.Errorf("bunstore: saving TOTP: %w", err)
	}
	if activeID != nil && *activeID == ID(cred.ID) {
		return totp.ErrTOTPReplayed
	}

	return totp.ErrTOTPNotEnrolled
}

func (s *TOTPStore) DeleteTOTP(ctx context.Context, userID uuid.UUID) error {
	_, err := s.db.NewDelete().Model((*totpModel)(nil)).Where("user_id = ?", ID(userID)).Exec(ctx)
	if err != nil {
		return fmt.Errorf("bunstore: deleting TOTP: %w", err)
	}

	return nil
}
