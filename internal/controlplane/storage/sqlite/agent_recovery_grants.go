package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

func (s *Store) PutAgentCertificateRecoveryGrant(ctx context.Context, grant storage.AgentCertificateRecoveryGrantRecord) error {
	var usedAt any
	if grant.UsedAt != nil {
		usedAt = toUnix(*grant.UsedAt)
	}
	var revokedAt any
	if grant.RevokedAt != nil {
		revokedAt = toUnix(*grant.RevokedAt)
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO agent_certificate_recovery_grants (agent_id, issued_by, issued_at_unix, expires_at_unix, used_at_unix, revoked_at_unix)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(agent_id) DO UPDATE SET
			issued_by = excluded.issued_by,
			issued_at_unix = excluded.issued_at_unix,
			expires_at_unix = excluded.expires_at_unix,
			used_at_unix = excluded.used_at_unix,
			revoked_at_unix = excluded.revoked_at_unix
	`, grant.AgentID, grant.IssuedBy, toUnix(grant.IssuedAt), toUnix(grant.ExpiresAt), usedAt, revokedAt)
	return err
}

func (s *Store) ListAgentCertificateRecoveryGrants(ctx context.Context) ([]storage.AgentCertificateRecoveryGrantRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT agent_id, issued_by, issued_at_unix, expires_at_unix, used_at_unix, revoked_at_unix
		FROM agent_certificate_recovery_grants
		ORDER BY issued_at_unix, agent_id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]storage.AgentCertificateRecoveryGrantRecord, 0)
	for rows.Next() {
		grant, err := scanAgentCertificateRecoveryGrantRow(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, grant)
	}

	return result, rows.Err()
}

func (s *Store) GetAgentCertificateRecoveryGrant(ctx context.Context, agentID string) (storage.AgentCertificateRecoveryGrantRecord, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT agent_id, issued_by, issued_at_unix, expires_at_unix, used_at_unix, revoked_at_unix
		FROM agent_certificate_recovery_grants
		WHERE agent_id = ?
	`, agentID)

	grant, err := scanAgentCertificateRecoveryGrantRow(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return storage.AgentCertificateRecoveryGrantRecord{}, storage.ErrNotFound
		}
		return storage.AgentCertificateRecoveryGrantRecord{}, err
	}

	return grant, nil
}

func (s *Store) UseAgentCertificateRecoveryGrant(ctx context.Context, agentID string, usedAt time.Time) (storage.AgentCertificateRecoveryGrantRecord, error) {
	tx, err := s.beginInternalTx(ctx)
	if err != nil {
		return storage.AgentCertificateRecoveryGrantRecord{}, err
	}
	defer tx.Rollback()

	row := tx.QueryRowContext(ctx, `
		SELECT agent_id, issued_by, issued_at_unix, expires_at_unix, used_at_unix, revoked_at_unix
		FROM agent_certificate_recovery_grants
		WHERE agent_id = ?
	`, agentID)

	grant, err := scanAgentCertificateRecoveryGrantRow(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return storage.AgentCertificateRecoveryGrantRecord{}, storage.ErrNotFound
		}
		return storage.AgentCertificateRecoveryGrantRecord{}, err
	}
	if grant.UsedAt != nil || grant.RevokedAt != nil {
		return storage.AgentCertificateRecoveryGrantRecord{}, storage.ErrConflict
	}

	result, err := tx.ExecContext(ctx, `
		UPDATE agent_certificate_recovery_grants
		SET used_at_unix = ?
		WHERE agent_id = ? AND used_at_unix IS NULL AND revoked_at_unix IS NULL
	`, toUnix(usedAt), agentID)
	if err != nil {
		return storage.AgentCertificateRecoveryGrantRecord{}, err
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return storage.AgentCertificateRecoveryGrantRecord{}, err
	}
	if rowsAffected == 0 {
		return storage.AgentCertificateRecoveryGrantRecord{}, storage.ErrConflict
	}
	if err := tx.Commit(); err != nil {
		return storage.AgentCertificateRecoveryGrantRecord{}, err
	}

	usedValue := usedAt.UTC()
	grant.UsedAt = &usedValue
	return grant, nil
}

func (s *Store) RevokeAgentCertificateRecoveryGrant(ctx context.Context, agentID string, revokedAt time.Time) (storage.AgentCertificateRecoveryGrantRecord, error) {
	tx, err := s.beginInternalTx(ctx)
	if err != nil {
		return storage.AgentCertificateRecoveryGrantRecord{}, err
	}
	defer tx.Rollback()

	row := tx.QueryRowContext(ctx, `
		SELECT agent_id, issued_by, issued_at_unix, expires_at_unix, used_at_unix, revoked_at_unix
		FROM agent_certificate_recovery_grants
		WHERE agent_id = ?
	`, agentID)

	grant, err := scanAgentCertificateRecoveryGrantRow(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return storage.AgentCertificateRecoveryGrantRecord{}, storage.ErrNotFound
		}
		return storage.AgentCertificateRecoveryGrantRecord{}, err
	}
	if grant.RevokedAt != nil || grant.UsedAt != nil {
		return grant, nil
	}

	result, err := tx.ExecContext(ctx, `
		UPDATE agent_certificate_recovery_grants
		SET revoked_at_unix = ?
		WHERE agent_id = ? AND used_at_unix IS NULL AND revoked_at_unix IS NULL
	`, toUnix(revokedAt), agentID)
	if err != nil {
		return storage.AgentCertificateRecoveryGrantRecord{}, err
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return storage.AgentCertificateRecoveryGrantRecord{}, err
	}
	if rowsAffected == 0 {
		return storage.AgentCertificateRecoveryGrantRecord{}, storage.ErrConflict
	}
	if err := tx.Commit(); err != nil {
		return storage.AgentCertificateRecoveryGrantRecord{}, err
	}

	revokedValue := revokedAt.UTC()
	grant.RevokedAt = &revokedValue
	return grant, nil
}

type agentCertificateRecoveryGrantScanner interface {
	Scan(dest ...any) error
}

func scanAgentCertificateRecoveryGrantRow(scanner agentCertificateRecoveryGrantScanner) (storage.AgentCertificateRecoveryGrantRecord, error) {
	var grant storage.AgentCertificateRecoveryGrantRecord
	var issuedAt int64
	var expiresAt int64
	var usedAt sql.NullInt64
	var revokedAt sql.NullInt64
	if err := scanner.Scan(&grant.AgentID, &grant.IssuedBy, &issuedAt, &expiresAt, &usedAt, &revokedAt); err != nil {
		return storage.AgentCertificateRecoveryGrantRecord{}, err
	}

	grant.IssuedAt = fromUnix(issuedAt)
	grant.ExpiresAt = fromUnix(expiresAt)
	if usedAt.Valid {
		timeValue := fromUnix(usedAt.Int64)
		grant.UsedAt = &timeValue
	}
	if revokedAt.Valid {
		timeValue := fromUnix(revokedAt.Int64)
		grant.RevokedAt = &timeValue
	}

	return grant, nil
}
