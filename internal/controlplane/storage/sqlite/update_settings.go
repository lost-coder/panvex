package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
)

func (s *Store) PutUpdateSettings(ctx context.Context, data json.RawMessage) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO update_config (key, value) VALUES ('settings', ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		string(data))
	return err
}

func (s *Store) GetUpdateSettings(ctx context.Context) (json.RawMessage, error) {
	var value string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM update_config WHERE key = 'settings'`).Scan(&value)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return json.RawMessage(value), nil
}

func (s *Store) PutUpdateState(ctx context.Context, data json.RawMessage) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO update_config (key, value) VALUES ('state', ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		string(data))
	return err
}

func (s *Store) GetUpdateState(ctx context.Context) (json.RawMessage, error) {
	var value string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM update_config WHERE key = 'state'`).Scan(&value)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return json.RawMessage(value), nil
}
