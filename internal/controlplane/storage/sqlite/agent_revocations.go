package sqlite

import (
	"context"
	"fmt"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

// PutAgentRevocation upserts a revocation. SQLite's ON CONFLICT requires
// naming the conflict column; we pick revoked_at as the primary update
// target but always max() cert_expires_at so a later (longer-lived) cert
// does not reduce the window.
func (s *Store) PutAgentRevocation(ctx context.Context, r storage.AgentRevocationRecord) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO agent_revocations (agent_id, revoked_at_unix, cert_expires_at_unix)
		VALUES (?, ?, ?)
		ON CONFLICT (agent_id) DO UPDATE SET
			revoked_at_unix      = excluded.revoked_at_unix,
			cert_expires_at_unix = MAX(agent_revocations.cert_expires_at_unix, excluded.cert_expires_at_unix)
	`, r.AgentID, r.RevokedAt.UTC().Unix(), r.CertExpiresAt.UTC().Unix())
	if err != nil {
		return fmt.Errorf("put agent revocation: %w", err)
	}
	return nil
}

func (s *Store) ListAgentRevocations(ctx context.Context) ([]storage.AgentRevocationRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT agent_id, revoked_at_unix, cert_expires_at_unix
		FROM agent_revocations
	`)
	if err != nil {
		return nil, fmt.Errorf("list agent revocations: %w", err)
	}
	defer rows.Close()

	var out []storage.AgentRevocationRecord
	for rows.Next() {
		var rec storage.AgentRevocationRecord
		var revokedUnix, expiresUnix int64
		if err := rows.Scan(&rec.AgentID, &revokedUnix, &expiresUnix); err != nil {
			return nil, fmt.Errorf("scan agent revocation: %w", err)
		}
		rec.RevokedAt = time.Unix(revokedUnix, 0).UTC()
		rec.CertExpiresAt = time.Unix(expiresUnix, 0).UTC()
		out = append(out, rec)
	}
	return out, rows.Err()
}

func (s *Store) DeleteExpiredAgentRevocations(ctx context.Context, before time.Time) (int64, error) {
	res, err := s.db.ExecContext(ctx, `
		DELETE FROM agent_revocations
		WHERE cert_expires_at_unix < ?
	`, before.UTC().Unix())
	if err != nil {
		return 0, fmt.Errorf("delete expired agent revocations: %w", err)
	}
	return res.RowsAffected()
}
