package postgres

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
			updated_at
		)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (scope) DO UPDATE
		SET ca_pem = EXCLUDED.ca_pem,
		    private_key_pem = EXCLUDED.private_key_pem,
		    updated_at = EXCLUDED.updated_at
	`, certificateAuthorityScope, authority.CAPEM, authority.PrivateKeyPEM, authority.UpdatedAt.UTC())
	return err
}

func (s *Store) GetCertificateAuthority(ctx context.Context) (storage.CertificateAuthorityRecord, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT
			ca_pem,
			private_key_pem,
			updated_at
		FROM certificate_authority
		WHERE scope = $1
	`, certificateAuthorityScope)

	var authority storage.CertificateAuthorityRecord
	if err := row.Scan(&authority.CAPEM, &authority.PrivateKeyPEM, &authority.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return storage.CertificateAuthorityRecord{}, storage.ErrNotFound
		}
		return storage.CertificateAuthorityRecord{}, err
	}

	authority.UpdatedAt = authority.UpdatedAt.UTC()
	return authority, nil
}
