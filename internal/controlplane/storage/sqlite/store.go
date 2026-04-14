package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/panvex/panvex/internal/controlplane/storage"
	_ "modernc.org/sqlite"
)

// Store persists control-plane records in a local SQLite database.
type Store struct {
	db *sql.DB
}

// Open opens a SQLite database file, applies the schema, and returns a storage backend.
func Open(dsn string) (*Store, error) {
	if err := ensureParentDirectory(dsn); err != nil {
		return nil, err
	}

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}

	db.SetMaxOpenConns(1)

	// PRAGMA foreign_keys must be set per-connection. This is safe as long as
	// MaxOpenConns is 1. If the pool size is ever increased, use a ConnectHook
	// or append _pragma=foreign_keys(1) to the DSN instead.
	if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
		db.Close()
		return nil, err
	}

	if err := Migrate(db); err != nil {
		db.Close()
		return nil, err
	}

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, err
	}

	return &Store{db: db}, nil
}

func ensureParentDirectory(dsn string) error {
	parent := filepath.Dir(dsn)
	if parent == "." || parent == "" {
		return nil
	}

	return os.MkdirAll(parent, 0o755)
}

// Ping verifies that the database connection is alive.
func (s *Store) Ping(ctx context.Context) error {
	return s.db.PingContext(ctx)
}

// Close releases the database handle owned by the store.
func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) PutUser(ctx context.Context, user storage.UserRecord) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO users (id, username, password_hash, role, totp_enabled, totp_secret, created_at_unix)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			username = excluded.username,
			password_hash = excluded.password_hash,
			role = excluded.role,
			totp_enabled = excluded.totp_enabled,
			totp_secret = excluded.totp_secret,
			created_at_unix = excluded.created_at_unix
	`, user.ID, user.Username, user.PasswordHash, user.Role, user.TotpEnabled, user.TotpSecret, toUnix(user.CreatedAt))
	return err
}

func (s *Store) GetUserByID(ctx context.Context, userID string) (storage.UserRecord, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, username, password_hash, role, totp_enabled, totp_secret, created_at_unix
		FROM users
		WHERE id = ?
	`, userID)

	var user storage.UserRecord
	var createdAt int64
	if err := row.Scan(&user.ID, &user.Username, &user.PasswordHash, &user.Role, &user.TotpEnabled, &user.TotpSecret, &createdAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return storage.UserRecord{}, storage.ErrNotFound
		}
		return storage.UserRecord{}, err
	}

	user.CreatedAt = fromUnix(createdAt)
	return user, nil
}

func (s *Store) GetUserByUsername(ctx context.Context, username string) (storage.UserRecord, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, username, password_hash, role, totp_enabled, totp_secret, created_at_unix
		FROM users
		WHERE username = ?
	`, username)

	var user storage.UserRecord
	var createdAt int64
	if err := row.Scan(&user.ID, &user.Username, &user.PasswordHash, &user.Role, &user.TotpEnabled, &user.TotpSecret, &createdAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return storage.UserRecord{}, storage.ErrNotFound
		}
		return storage.UserRecord{}, err
	}

	user.CreatedAt = fromUnix(createdAt)
	return user, nil
}

func (s *Store) ListUsers(ctx context.Context) ([]storage.UserRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, username, password_hash, role, totp_enabled, totp_secret, created_at_unix
		FROM users
		ORDER BY created_at_unix, id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]storage.UserRecord, 0)
	for rows.Next() {
		var user storage.UserRecord
		var createdAt int64
		if err := rows.Scan(&user.ID, &user.Username, &user.PasswordHash, &user.Role, &user.TotpEnabled, &user.TotpSecret, &createdAt); err != nil {
			return nil, err
		}
		user.CreatedAt = fromUnix(createdAt)
		result = append(result, user)
	}

	return result, rows.Err()
}

func (s *Store) PutFleetGroup(ctx context.Context, group storage.FleetGroupRecord) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO fleet_groups (id, name, created_at_unix)
		VALUES (?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			name = excluded.name,
			created_at_unix = excluded.created_at_unix
	`, group.ID, group.Name, toUnix(group.CreatedAt))
	return err
}

func (s *Store) ListFleetGroups(ctx context.Context) ([]storage.FleetGroupRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, created_at_unix
		FROM fleet_groups
		ORDER BY created_at_unix, id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]storage.FleetGroupRecord, 0)
	for rows.Next() {
		var group storage.FleetGroupRecord
		var createdAt int64
		if err := rows.Scan(&group.ID, &group.Name, &createdAt); err != nil {
			return nil, err
		}
		group.CreatedAt = fromUnix(createdAt)
		result = append(result, group)
	}

	return result, rows.Err()
}

func (s *Store) PutAgent(ctx context.Context, agent storage.AgentRecord) error {
	var fleetGroupID sql.NullString
	if agent.FleetGroupID != "" {
		fleetGroupID.Valid = true
		fleetGroupID.String = agent.FleetGroupID
	}

	var certIssuedAtUnix sql.NullInt64
	if agent.CertIssuedAt != nil {
		certIssuedAtUnix.Valid = true
		certIssuedAtUnix.Int64 = agent.CertIssuedAt.UTC().Unix()
	}
	var certExpiresAtUnix sql.NullInt64
	if agent.CertExpiresAt != nil {
		certExpiresAtUnix.Valid = true
		certExpiresAtUnix.Int64 = agent.CertExpiresAt.UTC().Unix()
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO agents (id, node_name, fleet_group_id, version, read_only, last_seen_at_unix, cert_issued_at_unix, cert_expires_at_unix)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			node_name = excluded.node_name,
			fleet_group_id = excluded.fleet_group_id,
			version = excluded.version,
			read_only = excluded.read_only,
			last_seen_at_unix = excluded.last_seen_at_unix,
			cert_issued_at_unix = excluded.cert_issued_at_unix,
			cert_expires_at_unix = excluded.cert_expires_at_unix
	`, agent.ID, agent.NodeName, fleetGroupID, agent.Version, boolToInt(agent.ReadOnly), toUnix(agent.LastSeenAt), certIssuedAtUnix, certExpiresAtUnix)
	return err
}

func (s *Store) ListAgents(ctx context.Context) ([]storage.AgentRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, node_name, fleet_group_id, version, read_only, last_seen_at_unix, cert_issued_at_unix, cert_expires_at_unix
		FROM agents
		ORDER BY last_seen_at_unix, id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]storage.AgentRecord, 0)
	for rows.Next() {
		var agent storage.AgentRecord
		var fleetGroupID sql.NullString
		var readOnly int
		var lastSeenAt int64
		var certIssuedAtUnix sql.NullInt64
		var certExpiresAtUnix sql.NullInt64
		if err := rows.Scan(&agent.ID, &agent.NodeName, &fleetGroupID, &agent.Version, &readOnly, &lastSeenAt, &certIssuedAtUnix, &certExpiresAtUnix); err != nil {
			return nil, err
		}
		if fleetGroupID.Valid {
			agent.FleetGroupID = fleetGroupID.String
		}
		agent.ReadOnly = intToBool(readOnly)
		agent.LastSeenAt = fromUnix(lastSeenAt)
		if certIssuedAtUnix.Valid {
			t := fromUnix(certIssuedAtUnix.Int64)
			agent.CertIssuedAt = &t
		}
		if certExpiresAtUnix.Valid {
			t := fromUnix(certExpiresAtUnix.Int64)
			agent.CertExpiresAt = &t
		}
		result = append(result, agent)
	}

	return result, rows.Err()
}

func (s *Store) PutInstance(ctx context.Context, instance storage.InstanceRecord) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO telemt_instances (id, agent_id, name, version, config_fingerprint, connected_users, read_only, updated_at_unix)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			agent_id = excluded.agent_id,
			name = excluded.name,
			version = excluded.version,
			config_fingerprint = excluded.config_fingerprint,
			connected_users = excluded.connected_users,
			read_only = excluded.read_only,
			updated_at_unix = excluded.updated_at_unix
	`, instance.ID, instance.AgentID, instance.Name, instance.Version, instance.ConfigFingerprint, instance.ConnectedUsers, boolToInt(instance.ReadOnly), toUnix(instance.UpdatedAt))
	return err
}

func (s *Store) ListInstances(ctx context.Context) ([]storage.InstanceRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, agent_id, name, version, config_fingerprint, connected_users, read_only, updated_at_unix
		FROM telemt_instances
		ORDER BY updated_at_unix, id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]storage.InstanceRecord, 0)
	for rows.Next() {
		var instance storage.InstanceRecord
		var readOnly int
		var updatedAt int64
		if err := rows.Scan(&instance.ID, &instance.AgentID, &instance.Name, &instance.Version, &instance.ConfigFingerprint, &instance.ConnectedUsers, &readOnly, &updatedAt); err != nil {
			return nil, err
		}
		instance.ReadOnly = intToBool(readOnly)
		instance.UpdatedAt = fromUnix(updatedAt)
		result = append(result, instance)
	}

	return result, rows.Err()
}

func (s *Store) PutJob(ctx context.Context, job storage.JobRecord) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO jobs (id, action, actor_id, status, created_at_unix, ttl_nanos, idempotency_key, payload_json)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			action = excluded.action,
			actor_id = excluded.actor_id,
			status = excluded.status,
			created_at_unix = excluded.created_at_unix,
			ttl_nanos = excluded.ttl_nanos,
			idempotency_key = excluded.idempotency_key,
			payload_json = excluded.payload_json
	`, job.ID, job.Action, job.ActorID, job.Status, toUnix(job.CreatedAt), job.TTL.Nanoseconds(), job.IdempotencyKey, job.PayloadJSON)
	return err
}

func (s *Store) GetJobByIdempotencyKey(ctx context.Context, idempotencyKey string) (storage.JobRecord, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, action, actor_id, status, created_at_unix, ttl_nanos, idempotency_key, payload_json
		FROM jobs
		WHERE idempotency_key = ?
	`, idempotencyKey)

	var job storage.JobRecord
	var createdAt int64
	var ttlNanos int64
	if err := row.Scan(&job.ID, &job.Action, &job.ActorID, &job.Status, &createdAt, &ttlNanos, &job.IdempotencyKey, &job.PayloadJSON); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return storage.JobRecord{}, storage.ErrNotFound
		}
		return storage.JobRecord{}, err
	}

	job.CreatedAt = fromUnix(createdAt)
	job.TTL = time.Duration(ttlNanos)
	return job, nil
}

func (s *Store) ListJobs(ctx context.Context) ([]storage.JobRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, action, actor_id, status, created_at_unix, ttl_nanos, idempotency_key, payload_json
		FROM jobs
		ORDER BY created_at_unix, id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]storage.JobRecord, 0)
	for rows.Next() {
		var job storage.JobRecord
		var createdAt int64
		var ttlNanos int64
		if err := rows.Scan(&job.ID, &job.Action, &job.ActorID, &job.Status, &createdAt, &ttlNanos, &job.IdempotencyKey, &job.PayloadJSON); err != nil {
			return nil, err
		}
		job.CreatedAt = fromUnix(createdAt)
		job.TTL = time.Duration(ttlNanos)
		result = append(result, job)
	}

	return result, rows.Err()
}

func (s *Store) PutJobTarget(ctx context.Context, target storage.JobTargetRecord) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO job_targets (job_id, agent_id, status, result_text, result_json, updated_at_unix)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(job_id, agent_id) DO UPDATE SET
			status = excluded.status,
			result_text = excluded.result_text,
			result_json = excluded.result_json,
			updated_at_unix = excluded.updated_at_unix
	`, target.JobID, target.AgentID, target.Status, target.ResultText, target.ResultJSON, toUnix(target.UpdatedAt))
	return err
}

func (s *Store) ListJobTargets(ctx context.Context, jobID string) ([]storage.JobTargetRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT job_id, agent_id, status, result_text, result_json, updated_at_unix
		FROM job_targets
		WHERE job_id = ?
		ORDER BY agent_id
	`, jobID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]storage.JobTargetRecord, 0)
	for rows.Next() {
		var target storage.JobTargetRecord
		var updatedAt int64
		if err := rows.Scan(&target.JobID, &target.AgentID, &target.Status, &target.ResultText, &target.ResultJSON, &updatedAt); err != nil {
			return nil, err
		}
		target.UpdatedAt = fromUnix(updatedAt)
		result = append(result, target)
	}

	return result, rows.Err()
}

func (s *Store) AppendAuditEvent(ctx context.Context, event storage.AuditEventRecord) error {
	detailsJSON, err := encodeJSON(event.Details)
	if err != nil {
		return err
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO audit_events (id, actor_id, action, target_id, created_at_unix, details_json)
		VALUES (?, ?, ?, ?, ?, ?)
	`, event.ID, event.ActorID, event.Action, event.TargetID, toUnix(event.CreatedAt), detailsJSON)
	return err
}

func (s *Store) ListAuditEvents(ctx context.Context, limit int) ([]storage.AuditEventRecord, error) {
	if limit <= 0 {
		limit = 1024
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, actor_id, action, target_id, created_at_unix, details_json
		FROM (SELECT * FROM audit_events ORDER BY created_at_unix DESC, id DESC LIMIT ?)
		ORDER BY created_at_unix, id
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]storage.AuditEventRecord, 0)
	for rows.Next() {
		var event storage.AuditEventRecord
		var createdAt int64
		var detailsJSON string
		if err := rows.Scan(&event.ID, &event.ActorID, &event.Action, &event.TargetID, &createdAt, &detailsJSON); err != nil {
			return nil, err
		}
		event.CreatedAt = fromUnix(createdAt)
		if err := decodeJSON(detailsJSON, &event.Details); err != nil {
			return nil, err
		}
		result = append(result, event)
	}

	return result, rows.Err()
}

func (s *Store) AppendMetricSnapshot(ctx context.Context, snapshot storage.MetricSnapshotRecord) error {
	valuesJSON, err := encodeJSON(snapshot.Values)
	if err != nil {
		return err
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO metric_snapshots (id, agent_id, instance_id, captured_at_unix, values_json)
		VALUES (?, ?, ?, ?, ?)
	`, snapshot.ID, snapshot.AgentID, snapshot.InstanceID, toUnix(snapshot.CapturedAt), valuesJSON)
	return err
}

func (s *Store) ListMetricSnapshots(ctx context.Context) ([]storage.MetricSnapshotRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, agent_id, instance_id, captured_at_unix, values_json
		FROM (SELECT * FROM metric_snapshots ORDER BY captured_at_unix DESC, id DESC LIMIT 512)
		ORDER BY captured_at_unix, id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]storage.MetricSnapshotRecord, 0)
	for rows.Next() {
		var snapshot storage.MetricSnapshotRecord
		var capturedAt int64
		var valuesJSON string
		if err := rows.Scan(&snapshot.ID, &snapshot.AgentID, &snapshot.InstanceID, &capturedAt, &valuesJSON); err != nil {
			return nil, err
		}
		snapshot.CapturedAt = fromUnix(capturedAt)
		if err := decodeJSON(valuesJSON, &snapshot.Values); err != nil {
			return nil, err
		}
		result = append(result, snapshot)
	}

	return result, rows.Err()
}

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
	tx, err := s.db.BeginTx(ctx, nil)
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
	tx, err := s.db.BeginTx(ctx, nil)
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
	tx, err := s.db.BeginTx(ctx, nil)
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
	tx, err := s.db.BeginTx(ctx, nil)
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

func toUnix(value time.Time) int64 {
	return value.UTC().Unix()
}

func fromUnix(value int64) time.Time {
	return time.Unix(value, 0).UTC()
}

func boolToInt(value bool) int {
	if value {
		return 1
	}

	return 0
}

func intToBool(value int) bool {
	return value != 0
}

func encodeJSON(value any) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", err
	}

	return string(data), nil
}

func decodeJSON[T any](value string, target *T) error {
	if value == "" {
		value = "{}"
	}

	return json.Unmarshal([]byte(value), target)
}
