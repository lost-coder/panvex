package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

// PutAgentRevocation upserts a revocation so repeated deregistrations are
// idempotent and cert_expires_at is kept fresh if the caller knows a newer
// cert existed.
func (s *Store) PutAgentRevocation(ctx context.Context, r storage.AgentRevocationRecord) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO agent_revocations (agent_id, revoked_at, cert_expires_at)
		VALUES ($1, $2, $3)
		ON CONFLICT (agent_id) DO UPDATE SET
			revoked_at      = EXCLUDED.revoked_at,
			cert_expires_at = GREATEST(agent_revocations.cert_expires_at, EXCLUDED.cert_expires_at)
	`, r.AgentID, r.RevokedAt.UTC(), r.CertExpiresAt.UTC())
	if err != nil {
		return fmt.Errorf("put agent revocation: %w", err)
	}
	return nil
}

func (s *Store) ListAgentRevocations(ctx context.Context) ([]storage.AgentRevocationRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT agent_id, revoked_at, cert_expires_at
		FROM agent_revocations
	`)
	if err != nil {
		return nil, fmt.Errorf("list agent revocations: %w", err)
	}
	defer rows.Close()

	var out []storage.AgentRevocationRecord
	for rows.Next() {
		var rec storage.AgentRevocationRecord
		var revoked, expires sql.NullTime
		if err := rows.Scan(&rec.AgentID, &revoked, &expires); err != nil {
			return nil, fmt.Errorf("scan agent revocation: %w", err)
		}
		if revoked.Valid {
			rec.RevokedAt = revoked.Time.UTC()
		}
		if expires.Valid {
			rec.CertExpiresAt = expires.Time.UTC()
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

// DeleteExpiredAgentRevocations removes entries whose cert has already
// expired — once the cert can no longer authenticate, the revocation entry
// is no longer useful and can shrink the table.
func (s *Store) DeleteExpiredAgentRevocations(ctx context.Context, before time.Time) (int64, error) {
	res, err := s.db.ExecContext(ctx, `
		DELETE FROM agent_revocations
		WHERE cert_expires_at < $1
	`, before.UTC())
	if err != nil {
		return 0, fmt.Errorf("delete expired agent revocations: %w", err)
	}
	return res.RowsAffected()
}
