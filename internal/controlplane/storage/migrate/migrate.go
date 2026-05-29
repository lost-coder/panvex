package migrate

import (
	"context"
	"errors"
	"fmt"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
	"github.com/lost-coder/panvex/internal/controlplane/storage/postgres"
	"github.com/lost-coder/panvex/internal/controlplane/storage/sqlite"
)

var (
	// ErrSourceDriver reports that the migration source backend is unsupported.
	ErrSourceDriver = errors.New("migration source must be sqlite")
	// ErrTargetDriver reports that the migration target backend is unsupported.
	ErrTargetDriver = errors.New("migration target must be postgres")
	// ErrTargetNotEmpty reports that the target backend already contains persistent state.
	ErrTargetNotEmpty = errors.New("migration target must be empty")
)

// Options describes the source and target backends for one offline migration run.
type Options struct {
	SourceDriver string
	SourceDSN    string
	TargetDriver string
	TargetDSN    string
}

// Summary reports how many persistent records the migration copied.
type Summary struct {
	Users                          int
	UserAppearance                 int
	FleetGroups                    int
	Agents                         int
	Instances                      int
	Jobs                           int
	JobTargets                     int
	AuditEvents                    int
	MetricSnapshots                int
	EnrollmentTokens               int
	AgentCertificateRecoveryGrants int
	Clients                        int
	ClientAssignments              int
	ClientDeployments              int
	PanelSettings                  int
	DiscoveredClients              int

	// Tier 1 + Tier 2 typed tables (finding L-5).
	AgentRevocations       int
	AgentFallbackState     int
	IntegrationProviders   int
	FleetGroupIntegrations int
	UserFleetGroupScopes   int
	ClientUsage            int
	ClientIPHistory        int
	Sessions               int
	UpdateConfig           int
	CPSecrets              int

	// Raw row-copy tables (ciphertext / out-of-MigrationStore registries).
	WebhookEndpoints int
	WebhookOutbox    int
	RuntimeSettings  int
}

// Run validates the backend pair, opens both stores, and performs one offline copy.
func Run(ctx context.Context, options Options) (Summary, error) {
	if options.SourceDriver != "sqlite" {
		return Summary{}, ErrSourceDriver
	}
	if options.TargetDriver != "postgres" {
		return Summary{}, ErrTargetDriver
	}
	if options.SourceDSN == "" {
		return Summary{}, errors.New("migration source dsn is required")
	}
	if options.TargetDSN == "" {
		return Summary{}, errors.New("migration target dsn is required")
	}

	source, err := sqlite.OpenContext(ctx, options.SourceDSN)
	if err != nil {
		return Summary{}, err
	}
	defer source.Close()

	target, err := postgres.OpenContext(ctx, options.TargetDSN)
	if err != nil {
		return Summary{}, err
	}
	defer target.Close()

	return copyStore(ctx, source, target)
}

// copyEntities streams every record returned by listFn through putFn and
// records the count via tally. Halts on the first error.
func copyEntities[T any](ctx context.Context, listFn func(context.Context) ([]T, error), putFn func(context.Context, T) error, tally func(int)) error {
	records, err := listFn(ctx)
	if err != nil {
		return err
	}
	for _, record := range records {
		if err := putFn(ctx, record); err != nil {
			return err
		}
	}
	tally(len(records))
	return nil
}

func copyJobsAndTargets(ctx context.Context, source, target storage.MigrationStore, summary *Summary) error {
	jobs, err := source.ListJobs(ctx)
	if err != nil {
		return err
	}
	for _, job := range jobs {
		if err := target.PutJob(ctx, job); err != nil {
			return err
		}
		targets, err := source.ListJobTargets(ctx, job.ID)
		if err != nil {
			return err
		}
		for _, targetRecord := range targets {
			if err := target.PutJobTarget(ctx, targetRecord); err != nil {
				return err
			}
		}
		summary.JobTargets += len(targets)
	}
	summary.Jobs = len(jobs)
	return nil
}

func copyClientsAndChildren(ctx context.Context, source, target storage.MigrationStore, summary *Summary) error {
	clients, err := source.ListClients(ctx)
	if err != nil {
		return err
	}
	for _, client := range clients {
		if err := copyClientWithChildren(ctx, source, target, client.ID, client, summary); err != nil {
			return err
		}
	}
	summary.Clients = len(clients)
	return nil
}

func copyClientWithChildren(ctx context.Context, source, target storage.MigrationStore, clientID string, client storage.ClientRecord, summary *Summary) error {
	if err := target.PutClient(ctx, client); err != nil {
		return err
	}
	assignmentCount, err := copyClientAssignments(ctx, source, target, clientID)
	if err != nil {
		return err
	}
	summary.ClientAssignments += assignmentCount

	deploymentCount, err := copyClientDeployments(ctx, source, target, clientID)
	if err != nil {
		return err
	}
	summary.ClientDeployments += deploymentCount
	return nil
}

func copyClientAssignments(ctx context.Context, source, target storage.MigrationStore, clientID string) (int, error) {
	assignments, err := source.ListClientAssignments(ctx, clientID)
	if err != nil {
		return 0, err
	}
	for _, assignment := range assignments {
		if err := target.PutClientAssignment(ctx, assignment); err != nil {
			return 0, err
		}
	}
	return len(assignments), nil
}

func copyClientDeployments(ctx context.Context, source, target storage.MigrationStore, clientID string) (int, error) {
	deployments, err := source.ListClientDeployments(ctx, clientID)
	if err != nil {
		return 0, err
	}
	for _, deployment := range deployments {
		if err := target.PutClientDeployment(ctx, deployment); err != nil {
			return 0, err
		}
	}
	return len(deployments), nil
}

// copySingletonAuthority returns the source authority record (if any) so the
// caller can verify it round-tripped to the target. errors.Is(_, ErrNotFound)
// is treated as "nothing to copy" rather than a hard failure.
func copySingletonAuthority(ctx context.Context, source, target storage.MigrationStore) (storage.CertificateAuthorityRecord, bool, error) {
	authority, err := source.GetCertificateAuthority(ctx)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return storage.CertificateAuthorityRecord{}, false, nil
		}
		return storage.CertificateAuthorityRecord{}, false, err
	}
	if err := target.PutCertificateAuthority(ctx, authority); err != nil {
		return storage.CertificateAuthorityRecord{}, false, err
	}
	return authority, true, nil
}

func copySingletonPanelSettings(ctx context.Context, source, target storage.MigrationStore, summary *Summary) error {
	settings, err := source.GetPanelSettings(ctx)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return nil
		}
		return err
	}
	if err := target.PutPanelSettings(ctx, settings); err != nil {
		return err
	}
	summary.PanelSettings = 1
	return nil
}

func verifyAuthorityRoundTrip(ctx context.Context, target storage.MigrationStore, expected storage.CertificateAuthorityRecord) error {
	stored, err := target.GetCertificateAuthority(ctx)
	if err != nil {
		return err
	}
	if stored.CAPEM != expected.CAPEM || stored.PrivateKeyPEM != expected.PrivateKeyPEM || !stored.UpdatedAt.Equal(expected.UpdatedAt) {
		return fmt.Errorf("migration verification failed: expected persisted certificate authority to round-trip")
	}
	return nil
}

func copyStore(ctx context.Context, source storage.MigrationStore, target storage.MigrationStore) (Summary, error) {
	if err := ensureTargetEmpty(ctx, target); err != nil {
		return Summary{}, err
	}

	summary := Summary{}

	if err := copyPrimaryEntities(ctx, source, target, &summary); err != nil {
		return Summary{}, err
	}
	if err := copyJobsAndTargets(ctx, source, target, &summary); err != nil {
		return Summary{}, err
	}
	if err := copyAuxiliaryEntities(ctx, source, target, &summary); err != nil {
		return Summary{}, err
	}
	if err := copyClientsAndChildren(ctx, source, target, &summary); err != nil {
		return Summary{}, err
	}
	if err := copyTierOneEntities(ctx, source, target, &summary); err != nil {
		return Summary{}, err
	}
	if err := copyPerParentEntities(ctx, source, target, &summary); err != nil {
		return Summary{}, err
	}
	if err := copyUpdateConfigSingletons(ctx, source, target, &summary); err != nil {
		return Summary{}, err
	}
	if err := copyRawTables(ctx, source, target, &summary); err != nil {
		return Summary{}, err
	}
	if err := copySingletonPanelSettings(ctx, source, target, &summary); err != nil {
		return Summary{}, err
	}

	authority, hasAuthority, err := copySingletonAuthority(ctx, source, target)
	if err != nil {
		return Summary{}, err
	}

	if err := verifyCounts(ctx, target, summary); err != nil {
		return Summary{}, err
	}
	if hasAuthority {
		if err := verifyAuthorityRoundTrip(ctx, target, authority); err != nil {
			return Summary{}, err
		}
	}

	return summary, nil
}

func copyPrimaryEntities(ctx context.Context, source, target storage.MigrationStore, summary *Summary) error {
	if err := copyEntities(ctx, source.ListUsers, target.PutUser, func(n int) { summary.Users = n }); err != nil {
		return err
	}
	if err := copyEntities(ctx, source.ListUserAppearances, target.PutUserAppearance, func(n int) { summary.UserAppearance = n }); err != nil {
		return err
	}
	if err := copyEntities(ctx, source.ListFleetGroups, target.PutFleetGroup, func(n int) { summary.FleetGroups = n }); err != nil {
		return err
	}
	if err := copyEntities(ctx, source.ListAgents, target.PutAgent, func(n int) { summary.Agents = n }); err != nil {
		return err
	}
	return copyEntities(ctx, source.ListInstances, target.PutInstance, func(n int) { summary.Instances = n })
}

func copyAuxiliaryEntities(ctx context.Context, source, target storage.MigrationStore, summary *Summary) error {
	if err := copyEntities(ctx,
		func(c context.Context) ([]storage.AuditEventRecord, error) { return source.ListAuditEvents(c, 0) },
		target.AppendAuditEvent,
		func(n int) { summary.AuditEvents = n },
	); err != nil {
		return err
	}
	if err := copyEntities(ctx, source.ListMetricSnapshots, target.AppendMetricSnapshot, func(n int) { summary.MetricSnapshots = n }); err != nil {
		return err
	}
	if err := copyEntities(ctx, source.ListEnrollmentTokens, target.PutEnrollmentToken, func(n int) { summary.EnrollmentTokens = n }); err != nil {
		return err
	}
	return copyEntities(ctx, source.ListAgentCertificateRecoveryGrants, target.PutAgentCertificateRecoveryGrant, func(n int) { summary.AgentCertificateRecoveryGrants = n })
}

func ensureTargetEmpty(ctx context.Context, target storage.MigrationStore) error {
	counts, err := listCounts(ctx, target)
	if err != nil {
		return err
	}
	// listCounts now populates every migrated-table field (typed + raw),
	// so any non-zero count means the target already holds persistent
	// state we must not overwrite. Comparing against the zero Summary
	// keeps this check in lock-step with the copy/verify paths — a new
	// migrated table cannot be forgotten here.
	if counts != (Summary{}) {
		return ErrTargetNotEmpty
	}
	if _, err := target.GetCertificateAuthority(ctx); err == nil {
		return ErrTargetNotEmpty
	} else if !errors.Is(err, storage.ErrNotFound) {
		return err
	}

	return nil
}

func verifyCounts(ctx context.Context, target storage.MigrationStore, expected Summary) error {
	actual, err := listCounts(ctx, target)
	if err != nil {
		return err
	}
	if actual != expected {
		return fmt.Errorf("migration verification failed: expected %+v, got %+v", expected, actual)
	}

	return nil
}

func listCounts(ctx context.Context, store storage.MigrationStore) (Summary, error) {
	var summary Summary

	if err := countPrimaryEntities(ctx, store, &summary); err != nil {
		return Summary{}, err
	}
	if err := countJobsAndTargets(ctx, store, &summary); err != nil {
		return Summary{}, err
	}
	if err := countAuxiliaryEntities(ctx, store, &summary); err != nil {
		return Summary{}, err
	}
	if err := countClientsAndChildren(ctx, store, &summary); err != nil {
		return Summary{}, err
	}
	if err := countTierOneEntities(ctx, store, &summary); err != nil {
		return Summary{}, err
	}
	if err := countPerParentEntities(ctx, store, &summary); err != nil {
		return Summary{}, err
	}
	if err := countUpdateConfigSingletons(ctx, store, &summary); err != nil {
		return Summary{}, err
	}
	if err := countRawTables(ctx, store, &summary); err != nil {
		return Summary{}, err
	}
	if err := countPanelSettings(ctx, store, &summary); err != nil {
		return Summary{}, err
	}

	return summary, nil
}

func countPrimaryEntities(ctx context.Context, store storage.MigrationStore, summary *Summary) error {
	users, err := store.ListUsers(ctx)
	if err != nil {
		return err
	}
	summary.Users = len(users)

	appearances, err := store.ListUserAppearances(ctx)
	if err != nil {
		return err
	}
	summary.UserAppearance = len(appearances)

	fleetGroups, err := store.ListFleetGroups(ctx)
	if err != nil {
		return err
	}
	summary.FleetGroups = len(fleetGroups)

	agents, err := store.ListAgents(ctx)
	if err != nil {
		return err
	}
	summary.Agents = len(agents)

	instances, err := store.ListInstances(ctx)
	if err != nil {
		return err
	}
	summary.Instances = len(instances)
	return nil
}

func countJobsAndTargets(ctx context.Context, store storage.MigrationStore, summary *Summary) error {
	jobs, err := store.ListJobs(ctx)
	if err != nil {
		return err
	}
	summary.Jobs = len(jobs)
	for _, job := range jobs {
		targets, err := store.ListJobTargets(ctx, job.ID)
		if err != nil {
			return err
		}
		summary.JobTargets += len(targets)
	}
	return nil
}

func countAuxiliaryEntities(ctx context.Context, store storage.MigrationStore, summary *Summary) error {
	auditEvents, err := store.ListAuditEvents(ctx, 0)
	if err != nil {
		return err
	}
	summary.AuditEvents = len(auditEvents)

	metricSnapshots, err := store.ListMetricSnapshots(ctx)
	if err != nil {
		return err
	}
	summary.MetricSnapshots = len(metricSnapshots)

	enrollmentTokens, err := store.ListEnrollmentTokens(ctx)
	if err != nil {
		return err
	}
	summary.EnrollmentTokens = len(enrollmentTokens)

	recoveryGrants, err := store.ListAgentCertificateRecoveryGrants(ctx)
	if err != nil {
		return err
	}
	summary.AgentCertificateRecoveryGrants = len(recoveryGrants)
	return nil
}

func countClientsAndChildren(ctx context.Context, store storage.MigrationStore, summary *Summary) error {
	clients, err := store.ListClients(ctx)
	if err != nil {
		return err
	}
	summary.Clients = len(clients)
	for _, client := range clients {
		assignments, err := store.ListClientAssignments(ctx, client.ID)
		if err != nil {
			return err
		}
		summary.ClientAssignments += len(assignments)

		deployments, err := store.ListClientDeployments(ctx, client.ID)
		if err != nil {
			return err
		}
		summary.ClientDeployments += len(deployments)
	}
	return nil
}

func countPanelSettings(ctx context.Context, store storage.MigrationStore, summary *Summary) error {
	if _, err := store.GetPanelSettings(ctx); err == nil {
		summary.PanelSettings = 1
	} else if !errors.Is(err, storage.ErrNotFound) {
		return err
	}
	return nil
}
