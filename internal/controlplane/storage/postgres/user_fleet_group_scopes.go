package postgres

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/lost-coder/panvex/internal/controlplane/storage"
	"github.com/lost-coder/panvex/internal/dbsqlc"
)

// R-Q-03 + R-S-14: routed through dbsqlc.

// ListUserFleetGroupScopes returns every fleet_group_id the user is
// scoped to. An empty slice means "global".
func (s *Store) ListUserFleetGroupScopes(ctx context.Context, userID string) ([]string, error) {
	rows, err := dbsqlc.New(s.db).ListUserFleetGroupScopes(ctx, userID)
	if err != nil {
		return nil, err
	}
	if rows == nil {
		return []string{}, nil
	}
	return rows, nil
}

// ListAllUserFleetGroupScopes returns every scope grant with provenance.
// Offline-migrate only. Uses raw SQL rather than dbsqlc because the
// migrate-complete listing has no other caller and adding a sqlc query
// would force a baseline regen.
func (s *Store) ListAllUserFleetGroupScopes(ctx context.Context) ([]storage.UserFleetGroupScopeRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT user_id, fleet_group_id, granted_by, granted_at
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
		if err := rows.Scan(&rec.UserID, &rec.FleetGroupID, &rec.GrantedBy, &rec.GrantedAt); err != nil {
			return nil, err
		}
		rec.GrantedAt = rec.GrantedAt.UTC()
		out = append(out, rec)
	}
	return out, rows.Err()
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

	q := dbsqlc.New(tx)
	if err := q.ClearUserFleetGroupScopes(ctx, userID); err != nil {
		return err
	}

	for _, id := range fleetGroupIDs {
		parsed, parseErr := uuid.Parse(id)
		if parseErr != nil {
			return parseErr
		}
		if err := q.InsertUserFleetGroupScope(ctx, dbsqlc.InsertUserFleetGroupScopeParams{
			UserID:       userID,
			FleetGroupID: parsed,
			GrantedAt:    grantedAt.UTC(),
			GrantedBy:    grantedBy,
		}); err != nil {
			return err
		}
	}

	return tx.Commit()
}
