package migrate

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
	"github.com/lost-coder/panvex/internal/controlplane/storage/postgres"
)

// wideTimeWindow brackets every plausible client_ip_history timestamp so
// the per-client list query returns the full set during migration.
var (
	wideWindowFrom = time.Unix(0, 0).UTC()
	wideWindowTo   = time.Date(2999, time.December, 31, 23, 59, 59, 0, time.UTC)
)

// copyTierOneEntities copies the flat typed tables added for finding L-5.
func copyTierOneEntities(ctx context.Context, source, target storage.MigrationStore, summary *Summary) error {
	if err := copyEntities(ctx, source.ListAgentRevocations, target.PutAgentRevocation, func(n int) { summary.AgentRevocations = n }); err != nil {
		return err
	}
	if err := copyEntities(ctx, source.ListAgentFallbackState, target.PutAgentFallbackState, func(n int) { summary.AgentFallbackState = n }); err != nil {
		return err
	}
	if err := copyEntities(ctx, source.ListIntegrationProviders, target.CreateIntegrationProvider, func(n int) { summary.IntegrationProviders = n }); err != nil {
		return err
	}
	if err := copyEntities(ctx, source.ListClientUsage, target.UpsertClientUsage, func(n int) { summary.ClientUsage = n }); err != nil {
		return err
	}
	if err := copyEntities(ctx, source.ListSessions, target.PutSession, func(n int) { summary.Sessions = n }); err != nil {
		return err
	}
	if err := copyEntities(ctx, source.ListDiscoveredClients, target.PutDiscoveredClient, func(n int) { summary.DiscoveredClients = n }); err != nil {
		return err
	}
	return copyCPSecrets(ctx, source, target, summary)
}

// copyCPSecrets copies the cp_secrets kv table verbatim (raw byte values).
func copyCPSecrets(ctx context.Context, source, target storage.MigrationStore, summary *Summary) error {
	secrets, err := source.ListCPSecrets(ctx)
	if err != nil {
		return err
	}
	for _, secret := range secrets {
		if err := target.PutCPSecret(ctx, secret.Key, secret.Value); err != nil {
			return err
		}
	}
	summary.CPSecrets = len(secrets)
	return nil
}

// copyPerParentEntities copies tables that must be iterated per parent
// row: fleet_group_integrations (per fleet group), user_fleet_group_scopes
// (per user, with full provenance), and client_ip_history (per client).
func copyPerParentEntities(ctx context.Context, source, target storage.MigrationStore, summary *Summary) error {
	if err := copyFleetGroupIntegrations(ctx, source, target, summary); err != nil {
		return err
	}
	if err := copyUserFleetGroupScopes(ctx, source, target, summary); err != nil {
		return err
	}
	return copyClientIPHistory(ctx, source, target, summary)
}

func copyFleetGroupIntegrations(ctx context.Context, source, target storage.MigrationStore, summary *Summary) error {
	groups, err := source.ListFleetGroups(ctx)
	if err != nil {
		return err
	}
	total := 0
	for _, group := range groups {
		integrations, err := source.ListFleetGroupIntegrations(ctx, group.ID)
		if err != nil {
			return err
		}
		for _, integration := range integrations {
			if err := target.CreateFleetGroupIntegration(ctx, integration); err != nil {
				return err
			}
		}
		total += len(integrations)
	}
	summary.FleetGroupIntegrations = total
	return nil
}

// copyUserFleetGroupScopes copies every scope grant with full provenance.
// It uses ListAllUserFleetGroupScopes (added for migration) instead of the
// per-user ListUserFleetGroupScopes because the latter drops granted_by /
// granted_at — losing them would corrupt the scope-grant audit trail. The
// grants are re-applied per user via SetUserFleetGroupScopes, which is the
// only typed write path; granted_by/granted_at are taken from the source
// rows so the copy round-trips byte-for-byte. (One user's grants always
// share the same granted_by/granted_at because SetUserFleetGroupScopes
// writes them as a unit, so grouping per user is lossless.)
func copyUserFleetGroupScopes(ctx context.Context, source, target storage.MigrationStore, summary *Summary) error {
	scopes, err := source.ListAllUserFleetGroupScopes(ctx)
	if err != nil {
		return err
	}

	type grant struct {
		ids       []string
		grantedBy string
		grantedAt time.Time
	}
	perUser := make(map[string]*grant)
	order := make([]string, 0)
	for _, s := range scopes {
		g, ok := perUser[s.UserID]
		if !ok {
			g = &grant{grantedBy: s.GrantedBy, grantedAt: s.GrantedAt}
			perUser[s.UserID] = g
			order = append(order, s.UserID)
		} else if g.grantedBy != s.GrantedBy || !g.grantedAt.Equal(s.GrantedAt) {
			// A user's scopes are written as one unit (single
			// granted_by/granted_at), so all of a user's rows must share that
			// provenance. Mixed values mean out-of-band edits — fail loudly
			// rather than silently collapsing onto the first row's provenance.
			return fmt.Errorf("migrate: user %q has mixed fleet-group-scope provenance (granted_by/granted_at differ across rows); resolve manually before migrating", s.UserID)
		}
		g.ids = append(g.ids, s.FleetGroupID)
	}

	for _, userID := range order {
		g := perUser[userID]
		if err := target.SetUserFleetGroupScopes(ctx, userID, g.ids, g.grantedBy, g.grantedAt); err != nil {
			return err
		}
	}
	summary.UserFleetGroupScopes = len(scopes)
	return nil
}

func copyClientIPHistory(ctx context.Context, source, target storage.MigrationStore, summary *Summary) error {
	clients, err := source.ListClients(ctx)
	if err != nil {
		return err
	}
	total := 0
	for _, client := range clients {
		history, err := source.ListClientIPHistory(ctx, client.ID, wideWindowFrom, wideWindowTo)
		if err != nil {
			return err
		}
		for _, record := range history {
			if err := target.UpsertClientIPHistory(ctx, record); err != nil {
				return err
			}
		}
		total += len(history)
	}
	summary.ClientIPHistory = total
	return nil
}

// copyUpdateConfigSingletons copies the four update_config kv rows. Each
// getter returns (nil, nil) when its row is absent (no ErrNotFound), so a
// nil/empty payload means "nothing to copy".
func copyUpdateConfigSingletons(ctx context.Context, source, target storage.MigrationStore, summary *Summary) error {
	type pair struct {
		get func(context.Context) (json.RawMessage, error)
		put func(context.Context, json.RawMessage) error
	}
	pairs := []pair{
		{source.GetUpdateSettings, target.PutUpdateSettings},
		{source.GetUpdateState, target.PutUpdateState},
		{source.GetGeoIPSettings, target.PutGeoIPSettings},
		{source.GetGeoIPState, target.PutGeoIPState},
	}

	count := 0
	for _, p := range pairs {
		data, err := p.get(ctx)
		if err != nil {
			return err
		}
		if len(data) == 0 {
			continue
		}
		if err := p.put(ctx, data); err != nil {
			return err
		}
		count++
	}
	summary.UpdateConfig = count
	return nil
}

// copyRawTables copies the tables that must be moved as raw rows: ciphertext-
// bearing webhook tables and the runtime_settings registry that lives in a
// separate store package. Values (including ciphertext) are copied verbatim.
func copyRawTables(ctx context.Context, source, target storage.MigrationStore, summary *Summary) error {
	dollar := targetUsesDollarPlaceholders(target)

	n, err := copyTableRaw(ctx, source, target, "webhook_endpoints", dollar)
	if err != nil {
		return err
	}
	summary.WebhookEndpoints = n

	n, err = copyTableRaw(ctx, source, target, "webhook_outbox", dollar)
	if err != nil {
		return err
	}
	summary.WebhookOutbox = n

	n, err = copyTableRaw(ctx, source, target, "runtime_settings", dollar)
	if err != nil {
		return err
	}
	summary.RuntimeSettings = n
	return nil
}

// targetUsesDollarPlaceholders reports whether the target backend expects
// PostgreSQL-style $N placeholders (vs SQLite-style ?).
func targetUsesDollarPlaceholders(target storage.MigrationStore) bool {
	_, ok := target.(*postgres.Store)
	return ok
}

func countTierOneEntities(ctx context.Context, store storage.MigrationStore, summary *Summary) error {
	revocations, err := store.ListAgentRevocations(ctx)
	if err != nil {
		return err
	}
	summary.AgentRevocations = len(revocations)

	fallback, err := store.ListAgentFallbackState(ctx)
	if err != nil {
		return err
	}
	summary.AgentFallbackState = len(fallback)

	providers, err := store.ListIntegrationProviders(ctx)
	if err != nil {
		return err
	}
	summary.IntegrationProviders = len(providers)

	usage, err := store.ListClientUsage(ctx)
	if err != nil {
		return err
	}
	summary.ClientUsage = len(usage)

	sessions, err := store.ListSessions(ctx)
	if err != nil {
		return err
	}
	summary.Sessions = len(sessions)

	discovered, err := store.ListDiscoveredClients(ctx)
	if err != nil {
		return err
	}
	summary.DiscoveredClients = len(discovered)

	secrets, err := store.ListCPSecrets(ctx)
	if err != nil {
		return err
	}
	summary.CPSecrets = len(secrets)
	return nil
}

func countPerParentEntities(ctx context.Context, store storage.MigrationStore, summary *Summary) error {
	groups, err := store.ListFleetGroups(ctx)
	if err != nil {
		return err
	}
	integrations := 0
	for _, group := range groups {
		fgi, err := store.ListFleetGroupIntegrations(ctx, group.ID)
		if err != nil {
			return err
		}
		integrations += len(fgi)
	}
	summary.FleetGroupIntegrations = integrations

	scopes, err := store.ListAllUserFleetGroupScopes(ctx)
	if err != nil {
		return err
	}
	summary.UserFleetGroupScopes = len(scopes)

	clients, err := store.ListClients(ctx)
	if err != nil {
		return err
	}
	ipHistory := 0
	for _, client := range clients {
		history, err := store.ListClientIPHistory(ctx, client.ID, wideWindowFrom, wideWindowTo)
		if err != nil {
			return err
		}
		ipHistory += len(history)
	}
	summary.ClientIPHistory = ipHistory
	return nil
}

func countUpdateConfigSingletons(ctx context.Context, store storage.MigrationStore, summary *Summary) error {
	getters := []func(context.Context) (json.RawMessage, error){
		store.GetUpdateSettings,
		store.GetUpdateState,
		store.GetGeoIPSettings,
		store.GetGeoIPState,
	}
	count := 0
	for _, get := range getters {
		data, err := get(ctx)
		if err != nil {
			return err
		}
		if len(data) > 0 {
			count++
		}
	}
	summary.UpdateConfig = count
	return nil
}

func countRawTables(ctx context.Context, store storage.MigrationStore, summary *Summary) error {
	n, err := countTableRaw(ctx, store, "webhook_endpoints")
	if err != nil {
		return err
	}
	summary.WebhookEndpoints = n

	n, err = countTableRaw(ctx, store, "webhook_outbox")
	if err != nil {
		return err
	}
	summary.WebhookOutbox = n

	n, err = countTableRaw(ctx, store, "runtime_settings")
	if err != nil {
		return err
	}
	summary.RuntimeSettings = n
	return nil
}
