package postgres

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// ListUserFleetGroupScopes returns every fleet_group_id the user is
// scoped to. An empty slice means "global" — see R-S-14.
func (s *Store) ListUserFleetGroupScopes(ctx context.Context, userID string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT fleet_group_id::text
		FROM user_fleet_group_scopes
		WHERE user_id = $1
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

// SetUserFleetGroupScopes replaces the user's scope set with the supplied
// list. Wrapped in a single transaction so a partially applied update
// cannot leave the operator stuck halfway between scopes.
func (s *Store) SetUserFleetGroupScopes(ctx context.Context, userID string, fleetGroupIDs []string, grantedBy string, grantedAt time.Time) error {
	tx, err := s.beginInternalTx(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM user_fleet_group_scopes WHERE user_id = $1`, userID); err != nil {
		return err
	}

	if len(fleetGroupIDs) > 0 {
		stmt := `
			INSERT INTO user_fleet_group_scopes (user_id, fleet_group_id, granted_at, granted_by)
			VALUES ($1, $2, $3, $4)
		`
		for _, id := range fleetGroupIDs {
			parsed, parseErr := uuid.Parse(id)
			if parseErr != nil {
				return parseErr
			}
			if _, err := tx.ExecContext(ctx, stmt, userID, parsed, grantedAt.UTC(), grantedBy); err != nil {
				return err
			}
		}
	}

	return tx.Commit()
}
