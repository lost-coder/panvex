package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

func (s *Store) GetCPSecret(ctx context.Context, key string) ([]byte, error) {
	row := s.db.QueryRowContext(ctx, `SELECT value FROM cp_secrets WHERE key = ?`, key)
	var value []byte
	if err := row.Scan(&value); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, storage.ErrNotFound
		}
		return nil, err
	}
	return value, nil
}

// ListCPSecrets enumerates every cp_secrets row for the offline migrate
// tooling. Values are returned verbatim as raw bytes.
func (s *Store) ListCPSecrets(ctx context.Context) ([]storage.CPSecretRecord, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT key, value, updated_at_unix FROM cp_secrets ORDER BY key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]storage.CPSecretRecord, 0)
	for rows.Next() {
		var rec storage.CPSecretRecord
		var updatedUnix int64
		if err := rows.Scan(&rec.Key, &rec.Value, &updatedUnix); err != nil {
			return nil, err
		}
		rec.UpdatedAt = fromUnix(updatedUnix)
		out = append(out, rec)
	}
	return out, rows.Err()
}

func (s *Store) PutCPSecret(ctx context.Context, key string, value []byte) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO cp_secrets (key, value, updated_at_unix)
		VALUES (?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET
			value = excluded.value,
			updated_at_unix = excluded.updated_at_unix
	`, key, value, toUnix(time.Now().UTC()))
	return err
}
