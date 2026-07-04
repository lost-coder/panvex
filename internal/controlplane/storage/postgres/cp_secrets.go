package postgres

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
	"github.com/lost-coder/panvex/internal/dbsqlc"
)

// R-Q-03: routed through dbsqlc so the column-level types are
// compile-time-checked.

func (s *Store) GetCPSecret(ctx context.Context, key string) ([]byte, error) {
	value, err := dbsqlc.New(s.db).GetCPSecret(ctx, key)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, storage.ErrNotFound
		}
		return nil, err
	}
	return value, nil
}

// ListCPSecrets enumerates every cp_secrets row for the offline migrate
// tooling. Values are returned verbatim as raw bytes. Uses raw SQL
// rather than dbsqlc because the migrate-complete listing has no other
// caller and adding a sqlc query would force a baseline regen.
func (s *Store) ListCPSecrets(ctx context.Context) ([]storage.CPSecretRecord, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT key, value, updated_at FROM cp_secrets ORDER BY key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]storage.CPSecretRecord, 0)
	for rows.Next() {
		var rec storage.CPSecretRecord
		if err := rows.Scan(&rec.Key, &rec.Value, &rec.UpdatedAt); err != nil {
			return nil, err
		}
		rec.UpdatedAt = rec.UpdatedAt.UTC()
		out = append(out, rec)
	}
	return out, rows.Err()
}

func (s *Store) PutCPSecret(ctx context.Context, key string, value []byte) error {
	return dbsqlc.New(s.db).UpsertCPSecret(ctx, dbsqlc.UpsertCPSecretParams{
		Key:       key,
		Value:     value,
		UpdatedAt: time.Now().UTC(),
	})
}
