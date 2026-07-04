package postgres

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
	"github.com/lost-coder/panvex/internal/dbsqlc"
)

// R-Q-03: routed through dbsqlc. Use/Revoke keep their internal-tx
// shape so the read-modify-write race is closed; the dbsqlc.Queries
// surface is bound to the tx via dbsqlc.New(tx).

func grantToParams(grant storage.AgentCertificateRecoveryGrantRecord) dbsqlc.UpsertAgentCertificateRecoveryGrantParams {
	params := dbsqlc.UpsertAgentCertificateRecoveryGrantParams{
		AgentID:   grant.AgentID,
		IssuedBy:  grant.IssuedBy,
		IssuedAt:  grant.IssuedAt.UTC(),
		ExpiresAt: grant.ExpiresAt.UTC(),
	}
	if grant.UsedAt != nil {
		params.UsedAt = sql.NullTime{Time: grant.UsedAt.UTC(), Valid: true}
	}
	if grant.RevokedAt != nil {
		params.RevokedAt = sql.NullTime{Time: grant.RevokedAt.UTC(), Valid: true}
	}
	return params
}

func grantFromRow(row dbsqlc.AgentCertificateRecoveryGrant) storage.AgentCertificateRecoveryGrantRecord {
	rec := storage.AgentCertificateRecoveryGrantRecord{
		AgentID:   row.AgentID,
		IssuedBy:  row.IssuedBy,
		IssuedAt:  row.IssuedAt.UTC(),
		ExpiresAt: row.ExpiresAt.UTC(),
	}
	if row.UsedAt.Valid {
		t := row.UsedAt.Time.UTC()
		rec.UsedAt = &t
	}
	if row.RevokedAt.Valid {
		t := row.RevokedAt.Time.UTC()
		rec.RevokedAt = &t
	}
	return rec
}

func (s *Store) PutAgentCertificateRecoveryGrant(ctx context.Context, grant storage.AgentCertificateRecoveryGrantRecord) error {
	return dbsqlc.New(s.db).UpsertAgentCertificateRecoveryGrant(ctx, grantToParams(grant))
}

func (s *Store) ListAgentCertificateRecoveryGrants(ctx context.Context) ([]storage.AgentCertificateRecoveryGrantRecord, error) {
	rows, err := dbsqlc.New(s.db).ListAgentCertificateRecoveryGrants(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]storage.AgentCertificateRecoveryGrantRecord, 0, len(rows))
	for _, row := range rows {
		out = append(out, grantFromRow(row))
	}
	return out, nil
}

func (s *Store) GetAgentCertificateRecoveryGrant(ctx context.Context, agentID string) (storage.AgentCertificateRecoveryGrantRecord, error) {
	row, err := dbsqlc.New(s.db).GetAgentCertificateRecoveryGrant(ctx, agentID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return storage.AgentCertificateRecoveryGrantRecord{}, storage.ErrNotFound
		}
		return storage.AgentCertificateRecoveryGrantRecord{}, err
	}
	return grantFromRow(row), nil
}

func (s *Store) UseAgentCertificateRecoveryGrant(ctx context.Context, agentID string, usedAt time.Time) (storage.AgentCertificateRecoveryGrantRecord, error) {
	tx, err := s.beginInternalTx(ctx)
	if err != nil {
		return storage.AgentCertificateRecoveryGrantRecord{}, err
	}
	defer tx.Rollback()

	q := dbsqlc.New(tx)
	row, err := q.GetAgentCertificateRecoveryGrant(ctx, agentID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return storage.AgentCertificateRecoveryGrantRecord{}, storage.ErrNotFound
		}
		return storage.AgentCertificateRecoveryGrantRecord{}, err
	}
	grant := grantFromRow(row)
	if grant.UsedAt != nil || grant.RevokedAt != nil {
		return storage.AgentCertificateRecoveryGrantRecord{}, storage.ErrConflict
	}

	rowsAffected, err := q.MarkAgentCertificateRecoveryGrantUsed(ctx, dbsqlc.MarkAgentCertificateRecoveryGrantUsedParams{
		UsedAt:  sql.NullTime{Time: usedAt.UTC(), Valid: true},
		AgentID: agentID,
	})
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

	q := dbsqlc.New(tx)
	row, err := q.GetAgentCertificateRecoveryGrant(ctx, agentID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return storage.AgentCertificateRecoveryGrantRecord{}, storage.ErrNotFound
		}
		return storage.AgentCertificateRecoveryGrantRecord{}, err
	}
	grant := grantFromRow(row)
	if grant.RevokedAt != nil || grant.UsedAt != nil {
		return grant, nil
	}

	rowsAffected, err := q.RevokeAgentCertificateRecoveryGrant(ctx, dbsqlc.RevokeAgentCertificateRecoveryGrantParams{
		RevokedAt: sql.NullTime{Time: revokedAt.UTC(), Valid: true},
		AgentID:   agentID,
	})
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
