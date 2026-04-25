package postgres

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

func (s *Store) PutEnrollmentToken(ctx context.Context, token storage.EnrollmentTokenRecord) error {
	var fleetGroupID sql.NullString
	if token.FleetGroupID != "" {
		fleetGroupID.Valid = true
		fleetGroupID.String = token.FleetGroupID
	}
	var consumedAt sql.NullTime
	if token.ConsumedAt != nil {
		consumedAt.Valid = true
		consumedAt.Time = token.ConsumedAt.UTC()
	}
	var revokedAt sql.NullTime
	if token.RevokedAt != nil {
		revokedAt.Valid = true
		revokedAt.Time = token.RevokedAt.UTC()
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO enrollment_tokens (value, fleet_group_id, issued_at, expires_at, consumed_at, revoked_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (value) DO UPDATE
		SET fleet_group_id = EXCLUDED.fleet_group_id,
		    issued_at = EXCLUDED.issued_at,
		    expires_at = EXCLUDED.expires_at,
		    consumed_at = EXCLUDED.consumed_at,
		    revoked_at = EXCLUDED.revoked_at
	`, token.Value, fleetGroupID, token.IssuedAt.UTC(), token.ExpiresAt.UTC(), consumedAt, revokedAt)
	return err
}

func (s *Store) ListEnrollmentTokens(ctx context.Context) ([]storage.EnrollmentTokenRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT value, fleet_group_id, issued_at, expires_at, consumed_at, revoked_at
		FROM enrollment_tokens
		ORDER BY issued_at, value
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]storage.EnrollmentTokenRecord, 0)
	for rows.Next() {
		var token storage.EnrollmentTokenRecord
		var fleetGroupID sql.NullString
		var consumedAt sql.NullTime
		var revokedAt sql.NullTime
		if err := rows.Scan(&token.Value, &fleetGroupID, &token.IssuedAt, &token.ExpiresAt, &consumedAt, &revokedAt); err != nil {
			return nil, err
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
		result = append(result, token)
	}

	return result, rows.Err()
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
