package sqlite

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
	var consumedAt sql.NullInt64
	if token.ConsumedAt != nil {
		consumedAt.Valid = true
		consumedAt.Int64 = toUnix(*token.ConsumedAt)
	}
	var revokedAt sql.NullInt64
	if token.RevokedAt != nil {
		revokedAt.Valid = true
		revokedAt.Int64 = toUnix(*token.RevokedAt)
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO enrollment_tokens (value, fleet_group_id, issued_at_unix, expires_at_unix, consumed_at_unix, revoked_at_unix)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(value) DO UPDATE SET
			fleet_group_id = excluded.fleet_group_id,
			issued_at_unix = excluded.issued_at_unix,
			expires_at_unix = excluded.expires_at_unix,
			consumed_at_unix = excluded.consumed_at_unix,
			revoked_at_unix = excluded.revoked_at_unix
	`, token.Value, fleetGroupID, toUnix(token.IssuedAt), toUnix(token.ExpiresAt), consumedAt, revokedAt)
	return err
}

func (s *Store) ListEnrollmentTokens(ctx context.Context) ([]storage.EnrollmentTokenRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT value, fleet_group_id, issued_at_unix, expires_at_unix, consumed_at_unix, revoked_at_unix
		FROM enrollment_tokens
		ORDER BY issued_at_unix, value
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]storage.EnrollmentTokenRecord, 0)
	for rows.Next() {
		var token storage.EnrollmentTokenRecord
		var fleetGroupID sql.NullString
		var issuedAt int64
		var expiresAt int64
		var consumedAt sql.NullInt64
		var revokedAt sql.NullInt64
		if err := rows.Scan(&token.Value, &fleetGroupID, &issuedAt, &expiresAt, &consumedAt, &revokedAt); err != nil {
			return nil, err
		}
		if fleetGroupID.Valid {
			token.FleetGroupID = fleetGroupID.String
		}
		token.IssuedAt = fromUnix(issuedAt)
		token.ExpiresAt = fromUnix(expiresAt)
		if consumedAt.Valid {
			timeValue := fromUnix(consumedAt.Int64)
			token.ConsumedAt = &timeValue
		}
		if revokedAt.Valid {
			timeValue := fromUnix(revokedAt.Int64)
			token.RevokedAt = &timeValue
		}
		result = append(result, token)
	}

	return result, rows.Err()
}

func (s *Store) GetEnrollmentToken(ctx context.Context, value string) (storage.EnrollmentTokenRecord, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT value, fleet_group_id, issued_at_unix, expires_at_unix, consumed_at_unix, revoked_at_unix
		FROM enrollment_tokens
		WHERE value = ?
	`, value)

	var token storage.EnrollmentTokenRecord
	var fleetGroupID sql.NullString
	var issuedAt int64
	var expiresAt int64
	var consumedAt sql.NullInt64
	var revokedAt sql.NullInt64
	if err := row.Scan(&token.Value, &fleetGroupID, &issuedAt, &expiresAt, &consumedAt, &revokedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return storage.EnrollmentTokenRecord{}, storage.ErrNotFound
		}
		return storage.EnrollmentTokenRecord{}, err
	}

	if fleetGroupID.Valid {
		token.FleetGroupID = fleetGroupID.String
	}
	token.IssuedAt = fromUnix(issuedAt)
	token.ExpiresAt = fromUnix(expiresAt)
	if consumedAt.Valid {
		timeValue := fromUnix(consumedAt.Int64)
		token.ConsumedAt = &timeValue
	}
	if revokedAt.Valid {
		timeValue := fromUnix(revokedAt.Int64)
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
		SELECT value, fleet_group_id, issued_at_unix, expires_at_unix, consumed_at_unix, revoked_at_unix
		FROM enrollment_tokens
		WHERE value = ?
	`, value)

	var token storage.EnrollmentTokenRecord
	var fleetGroupID sql.NullString
	var issuedAt int64
	var expiresAt int64
	var storedConsumedAt sql.NullInt64
	var storedRevokedAt sql.NullInt64
	if err := row.Scan(&token.Value, &fleetGroupID, &issuedAt, &expiresAt, &storedConsumedAt, &storedRevokedAt); err != nil {
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
		SET consumed_at_unix = ?
		WHERE value = ? AND consumed_at_unix IS NULL AND revoked_at_unix IS NULL
	`, toUnix(consumedAt), value)
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

	token.IssuedAt = fromUnix(issuedAt)
	token.ExpiresAt = fromUnix(expiresAt)
	token.ConsumedAt = &consumedAt
	return token, nil
}

func (s *Store) RevokeEnrollmentToken(ctx context.Context, value string, revokedAt time.Time) (storage.EnrollmentTokenRecord, error) {
	tx, err := s.beginInternalTx(ctx)
	if err != nil {
		return storage.EnrollmentTokenRecord{}, err
	}
	defer tx.Rollback()

	row := tx.QueryRowContext(ctx, `
		SELECT value, fleet_group_id, issued_at_unix, expires_at_unix, consumed_at_unix, revoked_at_unix
		FROM enrollment_tokens
		WHERE value = ?
	`, value)

	var token storage.EnrollmentTokenRecord
	var fleetGroupID sql.NullString
	var issuedAt int64
	var expiresAt int64
	var storedConsumedAt sql.NullInt64
	var storedRevokedAt sql.NullInt64
	if err := row.Scan(&token.Value, &fleetGroupID, &issuedAt, &expiresAt, &storedConsumedAt, &storedRevokedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return storage.EnrollmentTokenRecord{}, storage.ErrNotFound
		}
		return storage.EnrollmentTokenRecord{}, err
	}

	if fleetGroupID.Valid {
		token.FleetGroupID = fleetGroupID.String
	}
	token.IssuedAt = fromUnix(issuedAt)
	token.ExpiresAt = fromUnix(expiresAt)
	if storedConsumedAt.Valid {
		timeValue := fromUnix(storedConsumedAt.Int64)
		token.ConsumedAt = &timeValue
	}
	if storedRevokedAt.Valid {
		timeValue := fromUnix(storedRevokedAt.Int64)
		token.RevokedAt = &timeValue
		return token, nil
	}
	if storedConsumedAt.Valid {
		return token, nil
	}

	result, err := tx.ExecContext(ctx, `
		UPDATE enrollment_tokens
		SET revoked_at_unix = ?
		WHERE value = ? AND consumed_at_unix IS NULL AND revoked_at_unix IS NULL
	`, toUnix(revokedAt), value)
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
