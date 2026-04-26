package postgres

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/lost-coder/panvex/internal/dbsqlc"
)

// R-Q-03 + R-S-14: routed through dbsqlc.

// ListUserFleetGroupScopes returns every fleet_group_id the user is
// scoped to. An empty slice means "global".
func (s *Store) ListUserFleetGroupScopes(ctx context.Context, userID string) ([]string, error) {
	if s.sqlDB == nil {
		return nil, errTxBoundStore
	}
	rows, err := dbsqlc.New(s.sqlDB).ListUserFleetGroupScopes(ctx, userID)
	if err != nil {
		return nil, err
	}
	if rows == nil {
		return []string{}, nil
	}
	return rows, nil
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
