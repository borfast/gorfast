package bunstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"time"
	"uuid"

	"github.com/borfast/sulis/passkey"
	"github.com/go-webauthn/webauthn/protocol"
	"github.com/uptrace/bun"
)

// ErrPasskeyCredentialExists is returned by SaveCredential when the ID or the
// WebAuthn credential ID is already stored.
var ErrPasskeyCredentialExists = errors.New("bunstore: passkey credential already exists")

// passkeyModel is the storage form of a passkey.Credential. SignCount is
// int64 so that a uint32 up to MaxUint32 is written as a positive number.
type passkeyModel struct {
	bun.BaseModel `bun:"table:passkey_credentials,alias:pc"`

	ID              ID                                `bun:"id,pk"`
	UserID          ID                                `bun:"user_id"`
	CredentialID    []byte                            `bun:"credential_id"`
	PublicKey       []byte                            `bun:"public_key"`
	AttestationType string                            `bun:"attestation_type"`
	AAGUID          []byte                            `bun:"aaguid"`
	SignCount       int64                             `bun:"sign_count"`
	Name            string                            `bun:"name"`
	Transports      []protocol.AuthenticatorTransport `bun:"transports"`
	BackupEligible  bool                              `bun:"backup_eligible"`
	BackupState     bool                              `bun:"backup_state"`
	LastUsedAt      *time.Time                        `bun:"last_used_at"`
	Discoverable    bool                              `bun:"discoverable"`
	CreatedAt       time.Time                         `bun:"created_at"`
}

func toPasskeyModel(c *passkey.Credential) *passkeyModel {
	m := &passkeyModel{
		ID:              ID(c.ID),
		UserID:          ID(c.UserID),
		CredentialID:    c.CredentialID,
		PublicKey:       c.PublicKey,
		AttestationType: c.AttestationType,
		AAGUID:          c.AAGUID,
		SignCount:       int64(c.SignCount),
		Name:            c.Name,
		Transports:      c.Transports,
		BackupEligible:  c.BackupEligible,
		BackupState:     c.BackupState,
		Discoverable:    c.Discoverable,
		CreatedAt:       c.CreatedAt.UTC(),
	}
	if c.LastUsedAt != nil {
		m.LastUsedAt = utcPtr(c.LastUsedAt)
	}

	return m
}

func fromPasskeyModel(m *passkeyModel) passkey.Credential {
	return passkey.Credential{
		ID:              uuid.UUID(m.ID),
		UserID:          uuid.UUID(m.UserID),
		CredentialID:    m.CredentialID,
		PublicKey:       m.PublicKey,
		AttestationType: m.AttestationType,
		AAGUID:          m.AAGUID,
		SignCount:       uint32(m.SignCount),
		Name:            m.Name,
		Transports:      m.Transports,
		BackupEligible:  m.BackupEligible,
		BackupState:     m.BackupState,
		LastUsedAt:      utcPtr(m.LastUsedAt),
		Discoverable:    m.Discoverable,
		CreatedAt:       m.CreatedAt.UTC(),
	}
}

// PasskeyStore implements passkey.Store.
type PasskeyStore struct {
	db bun.IDB
}

var _ passkey.Store = (*PasskeyStore)(nil)

// NewPasskeyStore returns a PasskeyStore reading and writing through db.
func NewPasskeyStore(db bun.IDB) *PasskeyStore {
	return &PasskeyStore{db: db}
}

// SaveCredential uses ON CONFLICT DO NOTHING, so a duplicate does not abort a
// caller's Postgres transaction.
func (s *PasskeyStore) SaveCredential(ctx context.Context, cred *passkey.Credential) error {
	res, err := s.db.NewInsert().Model(toPasskeyModel(cred)).On("CONFLICT DO NOTHING").Exec(ctx)
	if isUniqueViolation(err) {
		return ErrPasskeyCredentialExists
	}
	if err != nil {
		return fmt.Errorf("bunstore: saving passkey credential: %w", err)
	}

	return requireOneRow(res, ErrPasskeyCredentialExists, "saving passkey credential")
}

func (s *PasskeyStore) GetCredentialsByUserID(ctx context.Context, userID uuid.UUID) ([]passkey.Credential, error) {
	var models []passkeyModel
	err := s.db.NewSelect().
		Model(&models).
		Where("user_id = ?", ID(userID)).
		OrderExpr("created_at, id").
		Scan(ctx)
	if err != nil {
		return nil, fmt.Errorf("bunstore: listing passkey credentials: %w", err)
	}

	creds := make([]passkey.Credential, 0, len(models))
	for i := range models {
		creds = append(creds, fromPasskeyModel(&models[i]))
	}

	return creds, nil
}

func (s *PasskeyStore) GetCredentialByID(ctx context.Context, credentialID []byte) (*passkey.Credential, error) {
	m := new(passkeyModel)
	err := s.db.NewSelect().Model(m).Where("credential_id = ?", credentialID).Scan(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, passkey.ErrPasskeyNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("bunstore: getting passkey credential: %w", err)
	}
	c := fromPasskeyModel(m)

	return &c, nil
}

func (s *PasskeyStore) UpdateCredentialAfterLogin(ctx context.Context, credentialID []byte, signCount uint32, backupState bool, lastUsedAt time.Time) error {
	res, err := s.db.NewUpdate().
		Model((*passkeyModel)(nil)).
		Set("sign_count = ?", int64(signCount)).
		Set("backup_state = ?", backupState).
		Set("last_used_at = ?", lastUsedAt.UTC()).
		Where("credential_id = ?", credentialID).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("bunstore: updating passkey credential after login: %w", err)
	}

	return requireOneRow(res, passkey.ErrPasskeyNotFound, "updating passkey credential after login")
}

// DeleteCredential locks and reads the user's credential IDs in one statement,
// so the IDs counted are the rows locked.
func (s *PasskeyStore) DeleteCredential(ctx context.Context, userID, id uuid.UUID, allowLast bool) error {
	err := s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		var ids []ID
		err := tx.NewUpdate().
			Model((*passkeyModel)(nil)).
			Set("user_id = user_id").
			Where("user_id = ?", ID(userID)).
			Returning("id").
			Scan(ctx, &ids)
		if err != nil {
			return err
		}
		if !slices.Contains(ids, ID(id)) {
			return passkey.ErrPasskeyNotFound
		}
		if len(ids) == 1 && !allowLast {
			return passkey.ErrLastCredential
		}

		_, err = tx.NewDelete().
			Model((*passkeyModel)(nil)).
			Where("id = ?", ID(id)).
			Where("user_id = ?", ID(userID)).
			Exec(ctx)
		return err
	})
	if errors.Is(err, passkey.ErrPasskeyNotFound) || errors.Is(err, passkey.ErrLastCredential) {
		return err
	}
	if err != nil {
		return fmt.Errorf("bunstore: deleting passkey credential: %w", err)
	}

	return nil
}

func (s *PasskeyStore) DeleteCredentialsByUserID(ctx context.Context, userID uuid.UUID) error {
	_, err := s.db.NewDelete().Model((*passkeyModel)(nil)).Where("user_id = ?", ID(userID)).Exec(ctx)
	if err != nil {
		return fmt.Errorf("bunstore: deleting passkey credentials: %w", err)
	}

	return nil
}

func (s *PasskeyStore) RenameCredential(ctx context.Context, userID, id uuid.UUID, name string) error {
	res, err := s.db.NewUpdate().
		Model((*passkeyModel)(nil)).
		Set("name = ?", name).
		Where("id = ?", ID(id)).
		Where("user_id = ?", ID(userID)).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("bunstore: renaming passkey credential: %w", err)
	}

	return requireOneRow(res, passkey.ErrPasskeyNotFound, "renaming passkey credential")
}
