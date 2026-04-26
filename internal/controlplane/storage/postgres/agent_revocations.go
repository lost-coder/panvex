package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
	"github.com/lost-coder/panvex/internal/dbsqlc"
)

// R-Q-03: routed through dbsqlc. revoked_at and cert_expires_at are
// NOT NULL on the schema (migration 0005), so the legacy sql.NullTime
// scaffolding is dropped here.

// PutAgentRevocation upserts a revocation so repeated deregistrations are
// idempotent and cert_expires_at is kept fresh if the caller knows a newer
// cert existed.
func (s *Store) PutAgentRevocation(ctx context.Context, r storage.AgentRevocationRecord) error {
	if s.sqlDB == nil {
		return errTxBoundStore
	}
	if err := dbsqlc.New(s.sqlDB).UpsertAgentRevocation(ctx, dbsqlc.UpsertAgentRevocationParams{
		AgentID:       r.AgentID,
		RevokedAt:     r.RevokedAt.UTC(),
		CertExpiresAt: r.CertExpiresAt.UTC(),
	}); err != nil {
		return fmt.Errorf("put agent revocation: %w", err)
	}
	return nil
}

func (s *Store) ListAgentRevocations(ctx context.Context) ([]storage.AgentRevocationRecord, error) {
	if s.sqlDB == nil {
		return nil, errTxBoundStore
	}
	rows, err := dbsqlc.New(s.sqlDB).ListAgentRevocations(ctx)
	if err != nil {
		return nil, fmt.Errorf("list agent revocations: %w", err)
	}
	out := make([]storage.AgentRevocationRecord, 0, len(rows))
	for _, row := range rows {
		out = append(out, storage.AgentRevocationRecord{
			AgentID:       row.AgentID,
			RevokedAt:     row.RevokedAt.UTC(),
			CertExpiresAt: row.CertExpiresAt.UTC(),
		})
	}
	return out, nil
}

// DeleteExpiredAgentRevocations removes entries whose cert has already
// expired — once the cert can no longer authenticate, the revocation
// entry is no longer useful and can shrink the table.
func (s *Store) DeleteExpiredAgentRevocations(ctx context.Context, before time.Time) (int64, error) {
	if s.sqlDB == nil {
		return 0, errTxBoundStore
	}
	n, err := dbsqlc.New(s.sqlDB).DeleteExpiredAgentRevocations(ctx, before.UTC())
	if err != nil {
		return 0, fmt.Errorf("delete expired agent revocations: %w", err)
	}
	return n, nil
}
