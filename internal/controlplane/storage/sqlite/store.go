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

func (s *Store) PutEnvironment(ctx context.Context, environment storage.EnvironmentRecord) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO environments (id, name, created_at_unix)
		VALUES (?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			name = excluded.name,
			created_at_unix = excluded.created_at_unix
	`, environment.ID, environment.Name, toUnix(environment.CreatedAt))
	return err
}

func (s *Store) ListEnvironments(ctx context.Context) ([]storage.EnvironmentRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, created_at_unix
		FROM environments
		ORDER BY created_at_unix, id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]storage.EnvironmentRecord, 0)
	for rows.Next() {
		var environment storage.EnvironmentRecord
		var createdAt int64
		if err := rows.Scan(&environment.ID, &environment.Name, &createdAt); err != nil {
			return nil, err
		}
		environment.CreatedAt = fromUnix(createdAt)
		result = append(result, environment)
	}

	return result, rows.Err()
}

func (s *Store) PutFleetGroup(ctx context.Context, group storage.FleetGroupRecord) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO fleet_groups (id, environment_id, name, created_at_unix)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			environment_id = excluded.environment_id,
			name = excluded.name,
			created_at_unix = excluded.created_at_unix
	`, group.ID, group.EnvironmentID, group.Name, toUnix(group.CreatedAt))
	return err
}

func (s *Store) ListFleetGroups(ctx context.Context) ([]storage.FleetGroupRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, environment_id, name, created_at_unix
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
		if err := rows.Scan(&group.ID, &group.EnvironmentID, &group.Name, &createdAt); err != nil {
			return nil, err
		}
		group.CreatedAt = fromUnix(createdAt)
		result = append(result, group)
	}

	return result, rows.Err()
}

func (s *Store) PutAgent(ctx context.Context, agent storage.AgentRecord) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO agents (id, node_name, environment_id, fleet_group_id, version, read_only, last_seen_at_unix)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			node_name = excluded.node_name,
			environment_id = excluded.environment_id,
			fleet_group_id = excluded.fleet_group_id,
			version = excluded.version,
			read_only = excluded.read_only,
			last_seen_at_unix = excluded.last_seen_at_unix
	`, agent.ID, agent.NodeName, agent.EnvironmentID, agent.FleetGroupID, agent.Version, boolToInt(agent.ReadOnly), toUnix(agent.LastSeenAt))
	return err
}

func (s *Store) ListAgents(ctx context.Context) ([]storage.AgentRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, node_name, environment_id, fleet_group_id, version, read_only, last_seen_at_unix
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
		var readOnly int
		var lastSeenAt int64
		if err := rows.Scan(&agent.ID, &agent.NodeName, &agent.EnvironmentID, &agent.FleetGroupID, &agent.Version, &readOnly, &lastSeenAt); err != nil {
			return nil, err
		}
		agent.ReadOnly = intToBool(readOnly)
		agent.LastSeenAt = fromUnix(lastSeenAt)
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
		INSERT INTO jobs (id, action, actor_id, status, created_at_unix, ttl_nanos, idempotency_key)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			action = excluded.action,
			actor_id = excluded.actor_id,
			status = excluded.status,
			created_at_unix = excluded.created_at_unix,
			ttl_nanos = excluded.ttl_nanos,
			idempotency_key = excluded.idempotency_key
	`, job.ID, job.Action, job.ActorID, job.Status, toUnix(job.CreatedAt), job.TTL.Nanoseconds(), job.IdempotencyKey)
	return err
}

func (s *Store) GetJobByIdempotencyKey(ctx context.Context, idempotencyKey string) (storage.JobRecord, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, action, actor_id, status, created_at_unix, ttl_nanos, idempotency_key
		FROM jobs
		WHERE idempotency_key = ?
	`, idempotencyKey)

	var job storage.JobRecord
	var createdAt int64
	var ttlNanos int64
	if err := row.Scan(&job.ID, &job.Action, &job.ActorID, &job.Status, &createdAt, &ttlNanos, &job.IdempotencyKey); err != nil {
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
		SELECT id, action, actor_id, status, created_at_unix, ttl_nanos, idempotency_key
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
		if err := rows.Scan(&job.ID, &job.Action, &job.ActorID, &job.Status, &createdAt, &ttlNanos, &job.IdempotencyKey); err != nil {
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
		INSERT INTO job_targets (job_id, agent_id, status, result_text, updated_at_unix)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(job_id, agent_id) DO UPDATE SET
			status = excluded.status,
			result_text = excluded.result_text,
			updated_at_unix = excluded.updated_at_unix
	`, target.JobID, target.AgentID, target.Status, target.ResultText, toUnix(target.UpdatedAt))
	return err
}

func (s *Store) ListJobTargets(ctx context.Context, jobID string) ([]storage.JobTargetRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT job_id, agent_id, status, result_text, updated_at_unix
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
		if err := rows.Scan(&target.JobID, &target.AgentID, &target.Status, &target.ResultText, &updatedAt); err != nil {
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

func (s *Store) ListAuditEvents(ctx context.Context) ([]storage.AuditEventRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, actor_id, action, target_id, created_at_unix, details_json
		FROM audit_events
		ORDER BY created_at_unix, id
	`)
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
		FROM metric_snapshots
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
	var consumedAt sql.NullInt64
	if token.ConsumedAt != nil {
		consumedAt.Valid = true
		consumedAt.Int64 = toUnix(*token.ConsumedAt)
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO enrollment_tokens (value, environment_id, fleet_group_id, issued_at_unix, expires_at_unix, consumed_at_unix)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(value) DO UPDATE SET
			environment_id = excluded.environment_id,
			fleet_group_id = excluded.fleet_group_id,
			issued_at_unix = excluded.issued_at_unix,
			expires_at_unix = excluded.expires_at_unix,
			consumed_at_unix = excluded.consumed_at_unix
	`, token.Value, token.EnvironmentID, token.FleetGroupID, toUnix(token.IssuedAt), toUnix(token.ExpiresAt), consumedAt)
	return err
}

func (s *Store) ListEnrollmentTokens(ctx context.Context) ([]storage.EnrollmentTokenRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT value, environment_id, fleet_group_id, issued_at_unix, expires_at_unix, consumed_at_unix
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
		var issuedAt int64
		var expiresAt int64
		var consumedAt sql.NullInt64
		if err := rows.Scan(&token.Value, &token.EnvironmentID, &token.FleetGroupID, &issuedAt, &expiresAt, &consumedAt); err != nil {
			return nil, err
		}
		token.IssuedAt = fromUnix(issuedAt)
		token.ExpiresAt = fromUnix(expiresAt)
		if consumedAt.Valid {
			timeValue := fromUnix(consumedAt.Int64)
			token.ConsumedAt = &timeValue
		}
		result = append(result, token)
	}

	return result, rows.Err()
}

func (s *Store) GetEnrollmentToken(ctx context.Context, value string) (storage.EnrollmentTokenRecord, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT value, environment_id, fleet_group_id, issued_at_unix, expires_at_unix, consumed_at_unix
		FROM enrollment_tokens
		WHERE value = ?
	`, value)

	var token storage.EnrollmentTokenRecord
	var issuedAt int64
	var expiresAt int64
	var consumedAt sql.NullInt64
	if err := row.Scan(&token.Value, &token.EnvironmentID, &token.FleetGroupID, &issuedAt, &expiresAt, &consumedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return storage.EnrollmentTokenRecord{}, storage.ErrNotFound
		}
		return storage.EnrollmentTokenRecord{}, err
	}

	token.IssuedAt = fromUnix(issuedAt)
	token.ExpiresAt = fromUnix(expiresAt)
	if consumedAt.Valid {
		timeValue := fromUnix(consumedAt.Int64)
		token.ConsumedAt = &timeValue
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
		SELECT value, environment_id, fleet_group_id, issued_at_unix, expires_at_unix, consumed_at_unix
		FROM enrollment_tokens
		WHERE value = ?
	`, value)

	var token storage.EnrollmentTokenRecord
	var issuedAt int64
	var expiresAt int64
	var storedConsumedAt sql.NullInt64
	if err := row.Scan(&token.Value, &token.EnvironmentID, &token.FleetGroupID, &issuedAt, &expiresAt, &storedConsumedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return storage.EnrollmentTokenRecord{}, storage.ErrNotFound
		}
		return storage.EnrollmentTokenRecord{}, err
	}

	if storedConsumedAt.Valid {
		return storage.EnrollmentTokenRecord{}, storage.ErrConflict
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE enrollment_tokens
		SET consumed_at_unix = ?
		WHERE value = ?
	`, toUnix(consumedAt), value); err != nil {
		return storage.EnrollmentTokenRecord{}, err
	}

	if err := tx.Commit(); err != nil {
		return storage.EnrollmentTokenRecord{}, err
	}

	token.IssuedAt = fromUnix(issuedAt)
	token.ExpiresAt = fromUnix(expiresAt)
	token.ConsumedAt = &consumedAt
	return token, nil
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
