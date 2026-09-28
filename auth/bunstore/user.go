package bunstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/borfast/sulis"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect"
)

type userModel struct {
	bun.BaseModel `bun:"table:users,alias:u"`

	ID                  string         `bun:"id,pk"`
	Email               string         `bun:"email"`
	PasswordHash        string         `bun:"password_hash"`
	CreatedAt           time.Time      `bun:"created_at"`
	UpdatedAt           time.Time      `bun:"updated_at"`
	Metadata            map[string]any `bun:"metadata"`
	EmailVerifiedAt     *time.Time     `bun:"email_verified_at"`
	PendingEmail        string         `bun:"pending_email"`
	DisabledAt          *time.Time     `bun:"disabled_at"`
	DisabledReason      string         `bun:"disabled_reason"`
	LockedUntil         *time.Time     `bun:"locked_until"`
	FailedLoginAttempts int            `bun:"failed_login_attempts"`
	Version             uint64         `bun:"version"`
}

func toUserModel(u *sulis.User) *userModel {
	return &userModel{
		ID:                  u.ID,
		Email:               u.Email,
		PasswordHash:        u.PasswordHash,
		CreatedAt:           u.CreatedAt,
		UpdatedAt:           u.UpdatedAt,
		Metadata:            u.Metadata,
		EmailVerifiedAt:     u.EmailVerifiedAt,
		PendingEmail:        u.PendingEmail,
		DisabledAt:          u.DisabledAt,
		DisabledReason:      u.DisabledReason,
		LockedUntil:         u.LockedUntil,
		FailedLoginAttempts: u.FailedLoginAttempts,
		Version:             u.Version,
	}
}

func fromUserModel(m *userModel) *sulis.User {
	return &sulis.User{
		ID:                  m.ID,
		Email:               m.Email,
		PasswordHash:        m.PasswordHash,
		CreatedAt:           m.CreatedAt.UTC(),
		UpdatedAt:           m.UpdatedAt.UTC(),
		Metadata:            m.Metadata,
		EmailVerifiedAt:     utcPtr(m.EmailVerifiedAt),
		PendingEmail:        m.PendingEmail,
		DisabledAt:          utcPtr(m.DisabledAt),
		DisabledReason:      m.DisabledReason,
		LockedUntil:         utcPtr(m.LockedUntil),
		FailedLoginAttempts: m.FailedLoginAttempts,
		Version:             m.Version,
	}
}

// UserStore implements sulis.UserStore.
type UserStore struct {
	db bun.IDB
}

var _ sulis.UserStore = (*UserStore)(nil)

// NewUserStore returns a UserStore reading and writing through db, which may
// be a connection or a transaction.
func NewUserStore(db bun.IDB) *UserStore {
	return &UserStore{db: db}
}

func (s *UserStore) CreateUser(ctx context.Context, user *sulis.User) error {
	res, err := s.db.NewInsert().Model(toUserModel(user)).On("CONFLICT DO NOTHING").Exec(ctx)
	if isUniqueViolation(err) {
		return sulis.ErrUserAlreadyExists
	}
	if err != nil {
		return fmt.Errorf("bunstore: creating user: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("bunstore: creating user: %w", err)
	}
	if n == 0 {
		return sulis.ErrUserAlreadyExists
	}

	return nil
}

func (s *UserStore) GetUserByID(ctx context.Context, id string) (*sulis.User, error) {
	return s.getUserBy(ctx, "id", "id = ?", id)
}

// GetUserByEmail compares case-insensitively, matching the unique index. On
// Postgres that means lower(email) = lower(?); on SQLite the email column's
// own COLLATE NOCASE already makes a plain equality case-insensitive.
func (s *UserStore) GetUserByEmail(ctx context.Context, email string) (*sulis.User, error) {
	where := "email = ?"
	if s.db.Dialect().Name() == dialect.PG {
		where = "lower(email) = lower(?)"
	}
	return s.getUserBy(ctx, "email", where, email)
}

func (s *UserStore) getUserBy(ctx context.Context, what, where, value string) (*sulis.User, error) {
	m := new(userModel)
	err := s.db.NewSelect().Model(m).Where(where, value).Scan(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, sulis.ErrUserNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("bunstore: reading user by %s: %w", what, err)
	}

	return fromUserModel(m), nil
}

// UpdateUser applies the write only while the stored version still matches
// user.Version, and leaves user.Version alone, matching memstore.
func (s *UserStore) UpdateUser(ctx context.Context, user *sulis.User) error {
	res, err := s.db.NewUpdate().
		Model(toUserModel(user)).
		Column("email", "password_hash", "updated_at", "metadata",
			"email_verified_at", "pending_email", "disabled_at",
			"disabled_reason", "locked_until", "failed_login_attempts").
		Set("version = version + 1").
		Where("id = ?", user.ID).
		Where("version = ?", user.Version).
		Exec(ctx)
	if isUniqueViolation(err) {
		return sulis.ErrUserAlreadyExists
	}
	if err != nil {
		return fmt.Errorf("bunstore: updating user: %w", err)
	}

	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("bunstore: updating user: %w", err)
	}
	if n == 0 {
		return s.explainFailedUpdate(ctx, user.ID)
	}

	return nil
}

// explainFailedUpdate tells a missing row from a stale version, since the
// UPDATE alone cannot and the contract names a different error for each.
func (s *UserStore) explainFailedUpdate(ctx context.Context, id string) error {
	exists, err := s.db.NewSelect().Model((*userModel)(nil)).Where("id = ?", id).Exists(ctx)
	if err != nil {
		return fmt.Errorf("bunstore: updating user: %w", err)
	}
	if !exists {
		return sulis.ErrUserNotFound
	}

	return sulis.ErrConcurrentUpdate
}

func (s *UserStore) DeleteUser(ctx context.Context, id string) error {
	_, err := s.db.NewDelete().Model((*userModel)(nil)).Where("id = ?", id).Exec(ctx)
	if err != nil {
		return fmt.Errorf("bunstore: deleting user: %w", err)
	}

	return nil
}
