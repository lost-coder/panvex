package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
	_ "github.com/jackc/pgx/v5/stdlib"
)

var (
	// ErrDSNRequired reports a missing PostgreSQL connection string.
	ErrDSNRequired = errors.New("postgres dsn is required")
)

// Store persists control-plane records in a PostgreSQL database.
type Store struct {
	db *sql.DB
}

// Open opens a PostgreSQL connection, applies the schema, and returns a storage backend.
func Open(dsn string) (*Store, error) {
	if strings.TrimSpace(dsn) == "" {
		return nil, ErrDSNRequired
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, err
	}

	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)

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
		INSERT INTO users (id, username, password_hash, role, totp_enabled, totp_secret, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (id) DO UPDATE
		SET username = EXCLUDED.username,
		    password_hash = EXCLUDED.password_hash,
		    role = EXCLUDED.role,
		    totp_enabled = EXCLUDED.totp_enabled,
		    totp_secret = EXCLUDED.totp_secret,
		    created_at = EXCLUDED.created_at
	`, user.ID, user.Username, user.PasswordHash, user.Role, user.TotpEnabled, user.TotpSecret, user.CreatedAt.UTC())
	return err
}

func (s *Store) GetUserByID(ctx context.Context, userID string) (storage.UserRecord, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, username, password_hash, role, totp_enabled, totp_secret, created_at
		FROM users
		WHERE id = $1
	`, userID)

	var user storage.UserRecord
	if err := row.Scan(&user.ID, &user.Username, &user.PasswordHash, &user.Role, &user.TotpEnabled, &user.TotpSecret, &user.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return storage.UserRecord{}, storage.ErrNotFound
		}
		return storage.UserRecord{}, err
	}

	user.CreatedAt = user.CreatedAt.UTC()
	return user, nil
}

func (s *Store) GetUserByUsername(ctx context.Context, username string) (storage.UserRecord, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, username, password_hash, role, totp_enabled, totp_secret, created_at
		FROM users
		WHERE username = $1
	`, username)

	var user storage.UserRecord
	if err := row.Scan(&user.ID, &user.Username, &user.PasswordHash, &user.Role, &user.TotpEnabled, &user.TotpSecret, &user.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return storage.UserRecord{}, storage.ErrNotFound
		}
		return storage.UserRecord{}, err
	}

	user.CreatedAt = user.CreatedAt.UTC()
	return user, nil
}

func (s *Store) ListUsers(ctx context.Context) ([]storage.UserRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, username, password_hash, role, totp_enabled, totp_secret, created_at
		FROM users
		ORDER BY created_at, id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]storage.UserRecord, 0)
	for rows.Next() {
		var user storage.UserRecord
		if err := rows.Scan(&user.ID, &user.Username, &user.PasswordHash, &user.Role, &user.TotpEnabled, &user.TotpSecret, &user.CreatedAt); err != nil {
			return nil, err
		}
		user.CreatedAt = user.CreatedAt.UTC()
		result = append(result, user)
	}

	return result, rows.Err()
}

func (s *Store) PutFleetGroup(ctx context.Context, group storage.FleetGroupRecord) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO fleet_groups (id, name, created_at)
		VALUES ($1, $2, $3)
		ON CONFLICT (id) DO UPDATE
		SET name = EXCLUDED.name,
		    created_at = EXCLUDED.created_at
	`, group.ID, group.Name, group.CreatedAt.UTC())
	return err
}

func (s *Store) ListFleetGroups(ctx context.Context) ([]storage.FleetGroupRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, created_at
		FROM fleet_groups
		ORDER BY created_at, id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]storage.FleetGroupRecord, 0)
	for rows.Next() {
		var group storage.FleetGroupRecord
		if err := rows.Scan(&group.ID, &group.Name, &group.CreatedAt); err != nil {
			return nil, err
		}
		group.CreatedAt = group.CreatedAt.UTC()
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

	var certIssuedAt sql.NullTime
	if agent.CertIssuedAt != nil {
		certIssuedAt.Valid = true
		certIssuedAt.Time = agent.CertIssuedAt.UTC()
	}
	var certExpiresAt sql.NullTime
	if agent.CertExpiresAt != nil {
		certExpiresAt.Valid = true
		certExpiresAt.Time = agent.CertExpiresAt.UTC()
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO agents (id, node_name, fleet_group_id, version, read_only, last_seen_at, cert_issued_at, cert_expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (id) DO UPDATE
		SET node_name = EXCLUDED.node_name,
		    fleet_group_id = EXCLUDED.fleet_group_id,
		    version = EXCLUDED.version,
		    read_only = EXCLUDED.read_only,
		    last_seen_at = EXCLUDED.last_seen_at,
		    cert_issued_at = EXCLUDED.cert_issued_at,
		    cert_expires_at = EXCLUDED.cert_expires_at
	`, agent.ID, agent.NodeName, fleetGroupID, agent.Version, agent.ReadOnly, agent.LastSeenAt.UTC(), certIssuedAt, certExpiresAt)
	return err
}

func (s *Store) ListAgents(ctx context.Context) ([]storage.AgentRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, node_name, fleet_group_id, version, read_only, last_seen_at, cert_issued_at, cert_expires_at
		FROM agents
		ORDER BY last_seen_at, id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]storage.AgentRecord, 0)
	for rows.Next() {
		var agent storage.AgentRecord
		var fleetGroupID sql.NullString
		var certIssuedAt sql.NullTime
		var certExpiresAt sql.NullTime
		if err := rows.Scan(&agent.ID, &agent.NodeName, &fleetGroupID, &agent.Version, &agent.ReadOnly, &agent.LastSeenAt, &certIssuedAt, &certExpiresAt); err != nil {
			return nil, err
		}
		if fleetGroupID.Valid {
			agent.FleetGroupID = fleetGroupID.String
		}
		agent.LastSeenAt = agent.LastSeenAt.UTC()
		if certIssuedAt.Valid {
			t := certIssuedAt.Time.UTC()
			agent.CertIssuedAt = &t
		}
		if certExpiresAt.Valid {
			t := certExpiresAt.Time.UTC()
			agent.CertExpiresAt = &t
		}
		result = append(result, agent)
	}

	return result, rows.Err()
}

func (s *Store) PutInstance(ctx context.Context, instance storage.InstanceRecord) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO telemt_instances (id, agent_id, name, version, config_fingerprint, connected_users, read_only, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (id) DO UPDATE
		SET agent_id = EXCLUDED.agent_id,
		    name = EXCLUDED.name,
		    version = EXCLUDED.version,
		    config_fingerprint = EXCLUDED.config_fingerprint,
		    connected_users = EXCLUDED.connected_users,
		    read_only = EXCLUDED.read_only,
		    updated_at = EXCLUDED.updated_at
	`, instance.ID, instance.AgentID, instance.Name, instance.Version, instance.ConfigFingerprint, instance.ConnectedUsers, instance.ReadOnly, instance.UpdatedAt.UTC())
	return err
}

func (s *Store) ListInstances(ctx context.Context) ([]storage.InstanceRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, agent_id, name, version, config_fingerprint, connected_users, read_only, updated_at
		FROM telemt_instances
		ORDER BY updated_at, id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]storage.InstanceRecord, 0)
	for rows.Next() {
		var instance storage.InstanceRecord
		if err := rows.Scan(&instance.ID, &instance.AgentID, &instance.Name, &instance.Version, &instance.ConfigFingerprint, &instance.ConnectedUsers, &instance.ReadOnly, &instance.UpdatedAt); err != nil {
			return nil, err
		}
		instance.UpdatedAt = instance.UpdatedAt.UTC()
		result = append(result, instance)
	}

	return result, rows.Err()
}

func (s *Store) PutJob(ctx context.Context, job storage.JobRecord) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO jobs (id, action, idempotency_key, actor_id, status, created_at, ttl_nanos, payload_json)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (id) DO UPDATE
		SET action = EXCLUDED.action,
		    idempotency_key = EXCLUDED.idempotency_key,
		    actor_id = EXCLUDED.actor_id,
		    status = EXCLUDED.status,
		    created_at = EXCLUDED.created_at,
		    ttl_nanos = EXCLUDED.ttl_nanos,
		    payload_json = EXCLUDED.payload_json
	`, job.ID, job.Action, job.IdempotencyKey, job.ActorID, job.Status, job.CreatedAt.UTC(), job.TTL.Nanoseconds(), job.PayloadJSON)
	return err
}

func (s *Store) GetJobByIdempotencyKey(ctx context.Context, idempotencyKey string) (storage.JobRecord, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, action, idempotency_key, actor_id, status, created_at, ttl_nanos, payload_json
		FROM jobs
		WHERE idempotency_key = $1
	`, idempotencyKey)

	var job storage.JobRecord
	var ttlNanos int64
	if err := row.Scan(&job.ID, &job.Action, &job.IdempotencyKey, &job.ActorID, &job.Status, &job.CreatedAt, &ttlNanos, &job.PayloadJSON); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return storage.JobRecord{}, storage.ErrNotFound
		}
		return storage.JobRecord{}, err
	}

	job.CreatedAt = job.CreatedAt.UTC()
	job.TTL = time.Duration(ttlNanos)
	return job, nil
}

func (s *Store) ListJobs(ctx context.Context) ([]storage.JobRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, action, idempotency_key, actor_id, status, created_at, ttl_nanos, payload_json
		FROM jobs
		ORDER BY created_at, id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]storage.JobRecord, 0)
	for rows.Next() {
		var job storage.JobRecord
		var ttlNanos int64
		if err := rows.Scan(&job.ID, &job.Action, &job.IdempotencyKey, &job.ActorID, &job.Status, &job.CreatedAt, &ttlNanos, &job.PayloadJSON); err != nil {
			return nil, err
		}
		job.CreatedAt = job.CreatedAt.UTC()
		job.TTL = time.Duration(ttlNanos)
		result = append(result, job)
	}

	return result, rows.Err()
}

func (s *Store) PutJobTarget(ctx context.Context, target storage.JobTargetRecord) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO job_targets (job_id, agent_id, status, result_text, result_json, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (job_id, agent_id) DO UPDATE
		SET status = EXCLUDED.status,
		    result_text = EXCLUDED.result_text,
		    result_json = EXCLUDED.result_json,
		    updated_at = EXCLUDED.updated_at
	`, target.JobID, target.AgentID, target.Status, target.ResultText, target.ResultJSON, target.UpdatedAt.UTC())
	return err
}

func (s *Store) ListJobTargets(ctx context.Context, jobID string) ([]storage.JobTargetRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT job_id, agent_id, status, result_text, result_json, updated_at
		FROM job_targets
		WHERE job_id = $1
		ORDER BY agent_id
	`, jobID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]storage.JobTargetRecord, 0)
	for rows.Next() {
		var target storage.JobTargetRecord
		if err := rows.Scan(&target.JobID, &target.AgentID, &target.Status, &target.ResultText, &target.ResultJSON, &target.UpdatedAt); err != nil {
			return nil, err
		}
		target.UpdatedAt = target.UpdatedAt.UTC()
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
		INSERT INTO audit_events (id, actor_id, action, target_id, details, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, event.ID, event.ActorID, event.Action, event.TargetID, detailsJSON, event.CreatedAt.UTC())
	return err
}

func (s *Store) ListAuditEvents(ctx context.Context, limit int) ([]storage.AuditEventRecord, error) {
	if limit <= 0 {
		limit = 1024
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, actor_id, action, target_id, details, created_at
		FROM (SELECT * FROM audit_events ORDER BY created_at DESC, id DESC LIMIT $1) sub
		ORDER BY created_at, id
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]storage.AuditEventRecord, 0)
	for rows.Next() {
		var event storage.AuditEventRecord
		var detailsJSON []byte
		if err := rows.Scan(&event.ID, &event.ActorID, &event.Action, &event.TargetID, &detailsJSON, &event.CreatedAt); err != nil {
			return nil, err
		}
		event.CreatedAt = event.CreatedAt.UTC()
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
		INSERT INTO metric_snapshots (id, agent_id, instance_id, captured_at, values)
		VALUES ($1, $2, $3, $4, $5)
	`, snapshot.ID, snapshot.AgentID, snapshot.InstanceID, snapshot.CapturedAt.UTC(), valuesJSON)
	return err
}

func (s *Store) ListMetricSnapshots(ctx context.Context) ([]storage.MetricSnapshotRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, agent_id, instance_id, captured_at, values
		FROM (SELECT * FROM metric_snapshots ORDER BY captured_at DESC, id DESC LIMIT 512) sub
		ORDER BY captured_at, id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]storage.MetricSnapshotRecord, 0)
	for rows.Next() {
		var snapshot storage.MetricSnapshotRecord
		var valuesJSON []byte
		if err := rows.Scan(&snapshot.ID, &snapshot.AgentID, &snapshot.InstanceID, &snapshot.CapturedAt, &valuesJSON); err != nil {
			return nil, err
		}
		snapshot.CapturedAt = snapshot.CapturedAt.UTC()
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
	tx, err := s.db.BeginTx(ctx, nil)
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
	tx, err := s.db.BeginTx(ctx, nil)
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
	tx, err := s.db.BeginTx(ctx, nil)
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
	tx, err := s.db.BeginTx(ctx, nil)
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

func encodeJSON(value any) ([]byte, error) {
	return json.Marshal(value)
}

func decodeJSON[T any](value []byte, target *T) error {
	if len(value) == 0 {
		value = []byte("{}")
	}

	return json.Unmarshal(value, target)
}
