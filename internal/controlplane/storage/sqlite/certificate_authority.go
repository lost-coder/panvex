package sqlite

import (
	"context"
	"database/sql"
	"errors"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

const certificateAuthorityScope = "control-plane-root-ca"

func (s *Store) PutCertificateAuthority(ctx context.Context, authority storage.CertificateAuthorityRecord) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO certificate_authority (
			scope,
			ca_pem,
			private_key_pem,
			updated_at_unix
		)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(scope) DO UPDATE SET
			ca_pem = excluded.ca_pem,
			private_key_pem = excluded.private_key_pem,
			updated_at_unix = excluded.updated_at_unix
	`, certificateAuthorityScope, authority.CAPEM, authority.PrivateKeyPEM, toUnix(authority.UpdatedAt))
	return err
}

func (s *Store) GetCertificateAuthority(ctx context.Context) (storage.CertificateAuthorityRecord, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT
			ca_pem,
			private_key_pem,
			updated_at_unix
		FROM certificate_authority
		WHERE scope = ?
	`, certificateAuthorityScope)

	var authority storage.CertificateAuthorityRecord
	var updatedAt int64
	if err := row.Scan(&authority.CAPEM, &authority.PrivateKeyPEM, &updatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return storage.CertificateAuthorityRecord{}, storage.ErrNotFound
		}
		return storage.CertificateAuthorityRecord{}, err
	}

	authority.UpdatedAt = fromUnix(updatedAt)
	return authority, nil
}
