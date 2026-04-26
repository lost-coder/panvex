package postgres

import (
	"context"
	"database/sql"
	"errors"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
	"github.com/lost-coder/panvex/internal/dbsqlc"
)

const certificateAuthorityScope = "control-plane-root-ca"

// R-Q-03: routed through dbsqlc.

func (s *Store) PutCertificateAuthority(ctx context.Context, authority storage.CertificateAuthorityRecord) error {
	if s.sqlDB == nil {
		return errTxBoundStore
	}
	return dbsqlc.New(s.sqlDB).UpsertCertificateAuthority(ctx, dbsqlc.UpsertCertificateAuthorityParams{
		Scope:         certificateAuthorityScope,
		CaPem:         authority.CAPEM,
		PrivateKeyPem: authority.PrivateKeyPEM,
		UpdatedAt:     authority.UpdatedAt.UTC(),
	})
}

func (s *Store) GetCertificateAuthority(ctx context.Context) (storage.CertificateAuthorityRecord, error) {
	if s.sqlDB == nil {
		return storage.CertificateAuthorityRecord{}, errTxBoundStore
	}
	row, err := dbsqlc.New(s.sqlDB).GetCertificateAuthority(ctx, certificateAuthorityScope)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return storage.CertificateAuthorityRecord{}, storage.ErrNotFound
		}
		return storage.CertificateAuthorityRecord{}, err
	}
	return storage.CertificateAuthorityRecord{
		CAPEM:         row.CaPem,
		PrivateKeyPEM: row.PrivateKeyPem,
		UpdatedAt:     row.UpdatedAt.UTC(),
	}, nil
}
