package sqlite

import (
	"context"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

// ListUserFleetGroupScopes — see storage.UserFleetGroupScopeStore.
// SQLite stores fleet_group_id as TEXT (not UUID) so the conversion is
// a simple Scan into string.
func (s *Store) ListUserFleetGroupScopes(ctx context.Context, userID string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT fleet_group_id
		FROM user_fleet_group_scopes
		WHERE user_id = ?
		ORDER BY fleet_group_id
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		result = append(result, id)
	}
	return result, rows.Err()
}

// ListAllUserFleetGroupScopes returns every scope grant with provenance.
// Offline-migrate only.
func (s *Store) ListAllUserFleetGroupScopes(ctx context.Context) ([]storage.UserFleetGroupScopeRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT user_id, fleet_group_id, granted_by, granted_at_unix
		FROM user_fleet_group_scopes
		ORDER BY user_id, fleet_group_id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]storage.UserFleetGroupScopeRecord, 0)
	for rows.Next() {
		var rec storage.UserFleetGroupScopeRecord
		var grantedAtUnix int64
		if err := rows.Scan(&rec.UserID, &rec.FleetGroupID, &rec.GrantedBy, &grantedAtUnix); err != nil {
			return nil, err
		}
		rec.GrantedAt = time.Unix(grantedAtUnix, 0).UTC()
		out = append(out, rec)
	}
	return out, rows.Err()
}

func (s *Store) SetUserFleetGroupScopes(ctx context.Context, userID string, fleetGroupIDs []string, grantedBy string, grantedAt time.Time) error {
	tx, err := s.beginInternalTx(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM user_fleet_group_scopes WHERE user_id = ?`, userID); err != nil {
		return err
	}

	if len(fleetGroupIDs) > 0 {
		stmt := `
			INSERT INTO user_fleet_group_scopes (user_id, fleet_group_id, granted_at_unix, granted_by)
			VALUES (?, ?, ?, ?)
		`
		grantedAtUnix := grantedAt.UTC().Unix()
		for _, id := range fleetGroupIDs {
			if _, err := tx.ExecContext(ctx, stmt, userID, id, grantedAtUnix, grantedBy); err != nil {
				return err
			}
		}
	}

	return tx.Commit()
}
