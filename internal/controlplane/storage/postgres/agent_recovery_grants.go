package postgres

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

func (s *Store) PutAgentCertificateRecoveryGrant(ctx context.Context, grant storage.AgentCertificateRecoveryGrantRecord) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO agent_certificate_recovery_grants (agent_id, issued_by, issued_at, expires_at, used_at, revoked_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (agent_id) DO UPDATE
		SET issued_by = EXCLUDED.issued_by,
		    issued_at = EXCLUDED.issued_at,
		    expires_at = EXCLUDED.expires_at,
		    used_at = EXCLUDED.used_at,
		    revoked_at = EXCLUDED.revoked_at
	`, grant.AgentID, grant.IssuedBy, grant.IssuedAt.UTC(), grant.ExpiresAt.UTC(), grant.UsedAt, grant.RevokedAt)
	return err
}

func (s *Store) ListAgentCertificateRecoveryGrants(ctx context.Context) ([]storage.AgentCertificateRecoveryGrantRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT agent_id, issued_by, issued_at, expires_at, used_at, revoked_at
		FROM agent_certificate_recovery_grants
		ORDER BY issued_at, agent_id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]storage.AgentCertificateRecoveryGrantRecord, 0)
	for rows.Next() {
		grant, err := scanAgentCertificateRecoveryGrantRecord(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, grant)
	}

	return result, rows.Err()
}

func (s *Store) GetAgentCertificateRecoveryGrant(ctx context.Context, agentID string) (storage.AgentCertificateRecoveryGrantRecord, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT agent_id, issued_by, issued_at, expires_at, used_at, revoked_at
		FROM agent_certificate_recovery_grants
		WHERE agent_id = $1
	`, agentID)

	grant, err := scanAgentCertificateRecoveryGrantRecord(row)
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
		SELECT agent_id, issued_by, issued_at, expires_at, used_at, revoked_at
		FROM agent_certificate_recovery_grants
		WHERE agent_id = $1
	`, agentID)

	grant, err := scanAgentCertificateRecoveryGrantRecord(row)
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
		SET used_at = $1
		WHERE agent_id = $2 AND used_at IS NULL AND revoked_at IS NULL
	`, usedAt.UTC(), agentID)
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
		SELECT agent_id, issued_by, issued_at, expires_at, used_at, revoked_at
		FROM agent_certificate_recovery_grants
		WHERE agent_id = $1
	`, agentID)

	grant, err := scanAgentCertificateRecoveryGrantRecord(row)
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
		SET revoked_at = $1
		WHERE agent_id = $2 AND used_at IS NULL AND revoked_at IS NULL
	`, revokedAt.UTC(), agentID)
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

type agentCertificateRecoveryGrantRecordScanner interface {
	Scan(dest ...any) error
}

func scanAgentCertificateRecoveryGrantRecord(scanner agentCertificateRecoveryGrantRecordScanner) (storage.AgentCertificateRecoveryGrantRecord, error) {
	var grant storage.AgentCertificateRecoveryGrantRecord
	var usedAt sql.NullTime
	var revokedAt sql.NullTime
	if err := scanner.Scan(&grant.AgentID, &grant.IssuedBy, &grant.IssuedAt, &grant.ExpiresAt, &usedAt, &revokedAt); err != nil {
		return storage.AgentCertificateRecoveryGrantRecord{}, err
	}

	grant.IssuedAt = grant.IssuedAt.UTC()
	grant.ExpiresAt = grant.ExpiresAt.UTC()
	if usedAt.Valid {
		timeValue := usedAt.Time.UTC()
		grant.UsedAt = &timeValue
	}
	if revokedAt.Valid {
		timeValue := revokedAt.Time.UTC()
		grant.RevokedAt = &timeValue
	}

	return grant, nil
}
