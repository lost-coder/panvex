package postgres

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/lost-coder/panvex/internal/controlplane/storage"
	"github.com/lost-coder/panvex/internal/dbsqlc"
)

// PutEnrollmentToken upserts one enrollment_tokens row.
//
// R-Q-03: routed through dbsqlc.UpsertEnrollmentToken so the postgres
// path gains compile-time type safety on every column. The dead
// value_hash column was dropped in migration 0044 (L-4).
func (s *Store) PutEnrollmentToken(ctx context.Context, token storage.EnrollmentTokenRecord) error {
	if s.sqlDB == nil {
		return errTxBoundStore
	}
	return dbsqlc.New(s.sqlDB).UpsertEnrollmentToken(ctx, enrollmentTokenToUpsertParams(token))
}

// enrollmentTokenToUpsertParams is the domain-DTO → dbsqlc params bridge.
func enrollmentTokenToUpsertParams(token storage.EnrollmentTokenRecord) dbsqlc.UpsertEnrollmentTokenParams {
	params := dbsqlc.UpsertEnrollmentTokenParams{
		Value:     token.Value,
		IssuedAt:  token.IssuedAt.UTC(),
		ExpiresAt: token.ExpiresAt.UTC(),
	}
	if token.FleetGroupID != "" {
		if id, err := uuid.Parse(token.FleetGroupID); err == nil {
			params.FleetGroupID = uuid.NullUUID{UUID: id, Valid: true}
		}
	}
	if token.ConsumedAt != nil {
		params.ConsumedAt = sql.NullTime{Time: token.ConsumedAt.UTC(), Valid: true}
	}
	if token.RevokedAt != nil {
		params.RevokedAt = sql.NullTime{Time: token.RevokedAt.UTC(), Valid: true}
	}
	return params
}

// ListEnrollmentTokens returns every token, ordered by issued_at + value
// for stable pagination.
//
// R-Q-03: routed through dbsqlc.ListEnrollmentTokens. Conversion from
// dbsqlc.EnrollmentToken to the storage shape lives in
// enrollmentTokenFromRow.
func (s *Store) ListEnrollmentTokens(ctx context.Context) ([]storage.EnrollmentTokenRecord, error) {
	if s.sqlDB == nil {
		return nil, errTxBoundStore
	}
	rows, err := dbsqlc.New(s.sqlDB).ListEnrollmentTokens(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]storage.EnrollmentTokenRecord, 0, len(rows))
	for _, row := range rows {
		result = append(result, enrollmentTokenFromRow(row))
	}
	return result, nil
}

// enrollmentTokenFromRow is the SQL-row → domain-DTO bridge.
func enrollmentTokenFromRow(row dbsqlc.EnrollmentToken) storage.EnrollmentTokenRecord {
	rec := storage.EnrollmentTokenRecord{
		Value:     row.Value,
		IssuedAt:  row.IssuedAt.UTC(),
		ExpiresAt: row.ExpiresAt.UTC(),
	}
	if row.FleetGroupID.Valid {
		rec.FleetGroupID = row.FleetGroupID.UUID.String()
	}
	if row.ConsumedAt.Valid {
		t := row.ConsumedAt.Time.UTC()
		rec.ConsumedAt = &t
	}
	if row.RevokedAt.Valid {
		t := row.RevokedAt.Time.UTC()
		rec.RevokedAt = &t
	}
	return rec
}

func (s *Store) GetEnrollmentToken(ctx context.Context, value string) (storage.EnrollmentTokenRecord, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT value, fleet_group_id, issued_at, expires_at, consumed_at, revoked_at
		FROM enrollment_tokens
		WHERE value = $1
	`, value)

	var token storage.EnrollmentTokenRecord
	var fleetGroupID sql.NullString
	var consumedAt sql.NullTime
	var revokedAt sql.NullTime
	if err := row.Scan(&token.Value, &fleetGroupID, &token.IssuedAt, &token.ExpiresAt, &consumedAt, &revokedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return storage.EnrollmentTokenRecord{}, storage.ErrNotFound
		}
		return storage.EnrollmentTokenRecord{}, err
	}

	if fleetGroupID.Valid {
		token.FleetGroupID = fleetGroupID.String
	}
	token.IssuedAt = token.IssuedAt.UTC()
	token.ExpiresAt = token.ExpiresAt.UTC()
	if consumedAt.Valid {
		timeValue := consumedAt.Time.UTC()
		token.ConsumedAt = &timeValue
	}
	if revokedAt.Valid {
		timeValue := revokedAt.Time.UTC()
		token.RevokedAt = &timeValue
	}

	return token, nil
}

func (s *Store) ConsumeEnrollmentToken(ctx context.Context, value string, consumedAt time.Time) (storage.EnrollmentTokenRecord, error) {
	tx, err := s.beginInternalTx(ctx)
	if err != nil {
		return storage.EnrollmentTokenRecord{}, err
	}
	defer tx.Rollback()

	row := tx.QueryRowContext(ctx, `
		SELECT value, fleet_group_id, issued_at, expires_at, consumed_at, revoked_at
		FROM enrollment_tokens
		WHERE value = $1
	`, value)

	var token storage.EnrollmentTokenRecord
	var fleetGroupID sql.NullString
	var storedConsumedAt sql.NullTime
	var storedRevokedAt sql.NullTime
	if err := row.Scan(&token.Value, &fleetGroupID, &token.IssuedAt, &token.ExpiresAt, &storedConsumedAt, &storedRevokedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return storage.EnrollmentTokenRecord{}, storage.ErrNotFound
		}
		return storage.EnrollmentTokenRecord{}, err
	}

	if storedConsumedAt.Valid || storedRevokedAt.Valid {
		return storage.EnrollmentTokenRecord{}, storage.ErrConflict
	}

	if fleetGroupID.Valid {
		token.FleetGroupID = fleetGroupID.String
	}

	result, err := tx.ExecContext(ctx, `
		UPDATE enrollment_tokens
		SET consumed_at = $1
		WHERE value = $2 AND consumed_at IS NULL AND revoked_at IS NULL
	`, consumedAt.UTC(), value)
	if err != nil {
		return storage.EnrollmentTokenRecord{}, err
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return storage.EnrollmentTokenRecord{}, err
	}
	if rowsAffected == 0 {
		return storage.EnrollmentTokenRecord{}, storage.ErrConflict
	}

	if err := tx.Commit(); err != nil {
		return storage.EnrollmentTokenRecord{}, err
	}

	token.IssuedAt = token.IssuedAt.UTC()
	token.ExpiresAt = token.ExpiresAt.UTC()
	consumedValue := consumedAt.UTC()
	token.ConsumedAt = &consumedValue
	return token, nil
}

func (s *Store) RevokeEnrollmentToken(ctx context.Context, value string, revokedAt time.Time) (storage.EnrollmentTokenRecord, error) {
	tx, err := s.beginInternalTx(ctx)
	if err != nil {
		return storage.EnrollmentTokenRecord{}, err
	}
	defer tx.Rollback()

	row := tx.QueryRowContext(ctx, `
		SELECT value, fleet_group_id, issued_at, expires_at, consumed_at, revoked_at
		FROM enrollment_tokens
		WHERE value = $1
	`, value)

	var token storage.EnrollmentTokenRecord
	var fleetGroupID sql.NullString
	var storedConsumedAt sql.NullTime
	var storedRevokedAt sql.NullTime
	if err := row.Scan(&token.Value, &fleetGroupID, &token.IssuedAt, &token.ExpiresAt, &storedConsumedAt, &storedRevokedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return storage.EnrollmentTokenRecord{}, storage.ErrNotFound
		}
		return storage.EnrollmentTokenRecord{}, err
	}

	if fleetGroupID.Valid {
		token.FleetGroupID = fleetGroupID.String
	}
	token.IssuedAt = token.IssuedAt.UTC()
	token.ExpiresAt = token.ExpiresAt.UTC()
	if storedConsumedAt.Valid {
		timeValue := storedConsumedAt.Time.UTC()
		token.ConsumedAt = &timeValue
	}
	if storedRevokedAt.Valid {
		timeValue := storedRevokedAt.Time.UTC()
		token.RevokedAt = &timeValue
		return token, nil
	}
	if storedConsumedAt.Valid {
		return token, nil
	}

	result, err := tx.ExecContext(ctx, `
		UPDATE enrollment_tokens
		SET revoked_at = $1
		WHERE value = $2 AND consumed_at IS NULL AND revoked_at IS NULL
	`, revokedAt.UTC(), value)
	if err != nil {
		return storage.EnrollmentTokenRecord{}, err
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return storage.EnrollmentTokenRecord{}, err
	}
	if rowsAffected == 0 {
		return storage.EnrollmentTokenRecord{}, storage.ErrConflict
	}

	if err := tx.Commit(); err != nil {
		return storage.EnrollmentTokenRecord{}, err
	}

	revokedValue := revokedAt.UTC()
	token.RevokedAt = &revokedValue
	return token, nil
}
