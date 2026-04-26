package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/lost-coder/panvex/internal/dbsqlc"
)

// R-Q-03: routed through dbsqlc. update_config is a tiny key/value
// table — the same UpsertUpdateConfig query handles both the
// "settings" and "state" keys.

func (s *Store) PutUpdateSettings(ctx context.Context, data json.RawMessage) error {
	return s.putUpdateConfig(ctx, "settings", data)
}

func (s *Store) GetUpdateSettings(ctx context.Context) (json.RawMessage, error) {
	return s.getUpdateConfig(ctx, "settings")
}

func (s *Store) PutUpdateState(ctx context.Context, data json.RawMessage) error {
	return s.putUpdateConfig(ctx, "state", data)
}

func (s *Store) GetUpdateState(ctx context.Context) (json.RawMessage, error) {
	return s.getUpdateConfig(ctx, "state")
}

func (s *Store) putUpdateConfig(ctx context.Context, key string, data json.RawMessage) error {
	if s.sqlDB == nil {
		return errTxBoundStore
	}
	return dbsqlc.New(s.sqlDB).UpsertUpdateConfig(ctx, dbsqlc.UpsertUpdateConfigParams{
		Key:   key,
		Value: string(data),
	})
}

func (s *Store) getUpdateConfig(ctx context.Context, key string) (json.RawMessage, error) {
	if s.sqlDB == nil {
		return nil, errTxBoundStore
	}
	value, err := dbsqlc.New(s.sqlDB).GetUpdateConfig(ctx, key)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return json.RawMessage(value), nil
}
