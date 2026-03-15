package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/panvex/panvex/internal/controlplane/storage"
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

func (s *Store) PutEnvironment(ctx context.Context, environment storage.EnvironmentRecord) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO environments (id, name, created_at)
		VALUES ($1, $2, $3)
		ON CONFLICT (id) DO UPDATE
		SET name = EXCLUDED.name,
		    created_at = EXCLUDED.created_at
	`, environment.ID, environment.Name, environment.CreatedAt.UTC())
	return err
}

func (s *Store) ListEnvironments(ctx context.Context) ([]storage.EnvironmentRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, created_at
		FROM environments
		ORDER BY created_at, id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]storage.EnvironmentRecord, 0)
	for rows.Next() {
		var environment storage.EnvironmentRecord
		if err := rows.Scan(&environment.ID, &environment.Name, &environment.CreatedAt); err != nil {
			return nil, err
		}
		environment.CreatedAt = environment.CreatedAt.UTC()
		result = append(result, environment)
	}

	return result, rows.Err()
}

func (s *Store) PutFleetGroup(ctx context.Context, group storage.FleetGroupRecord) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO fleet_groups (id, environment_id, name, created_at)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (id) DO UPDATE
		SET environment_id = EXCLUDED.environment_id,
		    name = EXCLUDED.name,
		    created_at = EXCLUDED.created_at
	`, group.ID, group.EnvironmentID, group.Name, group.CreatedAt.UTC())
	return err
}

func (s *Store) ListFleetGroups(ctx context.Context) ([]storage.FleetGroupRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, environment_id, name, created_at
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
		if err := rows.Scan(&group.ID, &group.EnvironmentID, &group.Name, &group.CreatedAt); err != nil {
			return nil, err
		}
		group.CreatedAt = group.CreatedAt.UTC()
		result = append(result, group)
	}

	return result, rows.Err()
}

func (s *Store) PutAgent(ctx context.Context, agent storage.AgentRecord) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO agents (id, node_name, environment_id, fleet_group_id, version, read_only, last_seen_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (id) DO UPDATE
		SET node_name = EXCLUDED.node_name,
		    environment_id = EXCLUDED.environment_id,
		    fleet_group_id = EXCLUDED.fleet_group_id,
		    version = EXCLUDED.version,
		    read_only = EXCLUDED.read_only,
		    last_seen_at = EXCLUDED.last_seen_at
	`, agent.ID, agent.NodeName, agent.EnvironmentID, agent.FleetGroupID, agent.Version, agent.ReadOnly, agent.LastSeenAt.UTC())
	return err
}

func (s *Store) ListAgents(ctx context.Context) ([]storage.AgentRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, node_name, environment_id, fleet_group_id, version, read_only, last_seen_at
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
		if err := rows.Scan(&agent.ID, &agent.NodeName, &agent.EnvironmentID, &agent.FleetGroupID, &agent.Version, &agent.ReadOnly, &agent.LastSeenAt); err != nil {
			return nil, err
		}
		agent.LastSeenAt = agent.LastSeenAt.UTC()
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
		INSERT INTO jobs (id, action, idempotency_key, actor_id, status, created_at, ttl_nanos)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (id) DO UPDATE
		SET action = EXCLUDED.action,
		    idempotency_key = EXCLUDED.idempotency_key,
		    actor_id = EXCLUDED.actor_id,
		    status = EXCLUDED.status,
		    created_at = EXCLUDED.created_at,
		    ttl_nanos = EXCLUDED.ttl_nanos
	`, job.ID, job.Action, job.IdempotencyKey, job.ActorID, job.Status, job.CreatedAt.UTC(), job.TTL.Nanoseconds())
	return err
}

func (s *Store) GetJobByIdempotencyKey(ctx context.Context, idempotencyKey string) (storage.JobRecord, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, action, idempotency_key, actor_id, status, created_at, ttl_nanos
		FROM jobs
		WHERE idempotency_key = $1
	`, idempotencyKey)

	var job storage.JobRecord
	var ttlNanos int64
	if err := row.Scan(&job.ID, &job.Action, &job.IdempotencyKey, &job.ActorID, &job.Status, &job.CreatedAt, &ttlNanos); err != nil {
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
		SELECT id, action, idempotency_key, actor_id, status, created_at, ttl_nanos
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
		if err := rows.Scan(&job.ID, &job.Action, &job.IdempotencyKey, &job.ActorID, &job.Status, &job.CreatedAt, &ttlNanos); err != nil {
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
		INSERT INTO job_targets (job_id, agent_id, status, result_text, updated_at)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (job_id, agent_id) DO UPDATE
		SET status = EXCLUDED.status,
		    result_text = EXCLUDED.result_text,
		    updated_at = EXCLUDED.updated_at
	`, target.JobID, target.AgentID, target.Status, target.ResultText, target.UpdatedAt.UTC())
	return err
}

func (s *Store) ListJobTargets(ctx context.Context, jobID string) ([]storage.JobTargetRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT job_id, agent_id, status, result_text, updated_at
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
		if err := rows.Scan(&target.JobID, &target.AgentID, &target.Status, &target.ResultText, &target.UpdatedAt); err != nil {
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

func (s *Store) ListAuditEvents(ctx context.Context) ([]storage.AuditEventRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, actor_id, action, target_id, details, created_at
		FROM audit_events
		ORDER BY created_at, id
	`)
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
		FROM metric_snapshots
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
	var consumedAt sql.NullTime
	if token.ConsumedAt != nil {
		consumedAt.Valid = true
		consumedAt.Time = token.ConsumedAt.UTC()
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO enrollment_tokens (value, environment_id, fleet_group_id, issued_at, expires_at, consumed_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (value) DO UPDATE
		SET environment_id = EXCLUDED.environment_id,
		    fleet_group_id = EXCLUDED.fleet_group_id,
		    issued_at = EXCLUDED.issued_at,
		    expires_at = EXCLUDED.expires_at,
		    consumed_at = EXCLUDED.consumed_at
	`, token.Value, token.EnvironmentID, token.FleetGroupID, token.IssuedAt.UTC(), token.ExpiresAt.UTC(), consumedAt)
	return err
}

func (s *Store) ListEnrollmentTokens(ctx context.Context) ([]storage.EnrollmentTokenRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT value, environment_id, fleet_group_id, issued_at, expires_at, consumed_at
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
		var consumedAt sql.NullTime
		if err := rows.Scan(&token.Value, &token.EnvironmentID, &token.FleetGroupID, &token.IssuedAt, &token.ExpiresAt, &consumedAt); err != nil {
			return nil, err
		}
		token.IssuedAt = token.IssuedAt.UTC()
		token.ExpiresAt = token.ExpiresAt.UTC()
		if consumedAt.Valid {
			timeValue := consumedAt.Time.UTC()
			token.ConsumedAt = &timeValue
		}
		result = append(result, token)
	}

	return result, rows.Err()
}

func (s *Store) GetEnrollmentToken(ctx context.Context, value string) (storage.EnrollmentTokenRecord, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT value, environment_id, fleet_group_id, issued_at, expires_at, consumed_at
		FROM enrollment_tokens
		WHERE value = $1
	`, value)

	var token storage.EnrollmentTokenRecord
	var consumedAt sql.NullTime
	if err := row.Scan(&token.Value, &token.EnvironmentID, &token.FleetGroupID, &token.IssuedAt, &token.ExpiresAt, &consumedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return storage.EnrollmentTokenRecord{}, storage.ErrNotFound
		}
		return storage.EnrollmentTokenRecord{}, err
	}

	token.IssuedAt = token.IssuedAt.UTC()
	token.ExpiresAt = token.ExpiresAt.UTC()
	if consumedAt.Valid {
		timeValue := consumedAt.Time.UTC()
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
		SELECT value, environment_id, fleet_group_id, issued_at, expires_at, consumed_at
		FROM enrollment_tokens
		WHERE value = $1
	`, value)

	var token storage.EnrollmentTokenRecord
	var storedConsumedAt sql.NullTime
	if err := row.Scan(&token.Value, &token.EnvironmentID, &token.FleetGroupID, &token.IssuedAt, &token.ExpiresAt, &storedConsumedAt); err != nil {
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
		SET consumed_at = $1
		WHERE value = $2
	`, consumedAt.UTC(), value); err != nil {
		return storage.EnrollmentTokenRecord{}, err
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

func encodeJSON(value any) ([]byte, error) {
	return json.Marshal(value)
}

func decodeJSON[T any](value []byte, target *T) error {
	if len(value) == 0 {
		value = []byte("{}")
	}

	return json.Unmarshal(value, target)
}
