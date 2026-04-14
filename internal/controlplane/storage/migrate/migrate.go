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
	Users             int
	UserAppearance    int
	FleetGroups       int
	Agents            int
	Instances         int
	Jobs              int
	JobTargets        int
	AuditEvents       int
	MetricSnapshots   int
	EnrollmentTokens  int
	AgentCertificateRecoveryGrants int
	Clients           int
	ClientAssignments int
	ClientDeployments int
	PanelSettings     int
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

	source, err := sqlite.Open(options.SourceDSN)
	if err != nil {
		return Summary{}, err
	}
	defer source.Close()

	target, err := postgres.Open(options.TargetDSN)
	if err != nil {
		return Summary{}, err
	}
	defer target.Close()

	return copyStore(ctx, source, target)
}

func copyStore(ctx context.Context, source storage.Store, target storage.Store) (Summary, error) {
	if err := ensureTargetEmpty(ctx, target); err != nil {
		return Summary{}, err
	}

	summary := Summary{}

	users, err := source.ListUsers(ctx)
	if err != nil {
		return Summary{}, err
	}
	for _, user := range users {
		if err := target.PutUser(ctx, user); err != nil {
			return Summary{}, err
		}
	}
	summary.Users = len(users)

	appearances, err := source.ListUserAppearances(ctx)
	if err != nil {
		return Summary{}, err
	}
	for _, appearance := range appearances {
		if err := target.PutUserAppearance(ctx, appearance); err != nil {
			return Summary{}, err
		}
		summary.UserAppearance++
	}

	fleetGroups, err := source.ListFleetGroups(ctx)
	if err != nil {
		return Summary{}, err
	}
	for _, group := range fleetGroups {
		if err := target.PutFleetGroup(ctx, group); err != nil {
			return Summary{}, err
		}
	}
	summary.FleetGroups = len(fleetGroups)

	agents, err := source.ListAgents(ctx)
	if err != nil {
		return Summary{}, err
	}
	for _, agent := range agents {
		if err := target.PutAgent(ctx, agent); err != nil {
			return Summary{}, err
		}
	}
	summary.Agents = len(agents)

	instances, err := source.ListInstances(ctx)
	if err != nil {
		return Summary{}, err
	}
	for _, instance := range instances {
		if err := target.PutInstance(ctx, instance); err != nil {
			return Summary{}, err
		}
	}
	summary.Instances = len(instances)

	jobs, err := source.ListJobs(ctx)
	if err != nil {
		return Summary{}, err
	}
	for _, job := range jobs {
		if err := target.PutJob(ctx, job); err != nil {
			return Summary{}, err
		}
		targets, err := source.ListJobTargets(ctx, job.ID)
		if err != nil {
			return Summary{}, err
		}
		for _, targetRecord := range targets {
			if err := target.PutJobTarget(ctx, targetRecord); err != nil {
				return Summary{}, err
			}
		}
		summary.JobTargets += len(targets)
	}
	summary.Jobs = len(jobs)

	auditEvents, err := source.ListAuditEvents(ctx, 0)
	if err != nil {
		return Summary{}, err
	}
	for _, event := range auditEvents {
		if err := target.AppendAuditEvent(ctx, event); err != nil {
			return Summary{}, err
		}
	}
	summary.AuditEvents = len(auditEvents)

	metricSnapshots, err := source.ListMetricSnapshots(ctx)
	if err != nil {
		return Summary{}, err
	}
	for _, snapshot := range metricSnapshots {
		if err := target.AppendMetricSnapshot(ctx, snapshot); err != nil {
			return Summary{}, err
		}
	}
	summary.MetricSnapshots = len(metricSnapshots)

	enrollmentTokens, err := source.ListEnrollmentTokens(ctx)
	if err != nil {
		return Summary{}, err
	}
	for _, token := range enrollmentTokens {
		if err := target.PutEnrollmentToken(ctx, token); err != nil {
			return Summary{}, err
		}
	}
	summary.EnrollmentTokens = len(enrollmentTokens)

	recoveryGrants, err := source.ListAgentCertificateRecoveryGrants(ctx)
	if err != nil {
		return Summary{}, err
	}
	for _, grant := range recoveryGrants {
		if err := target.PutAgentCertificateRecoveryGrant(ctx, grant); err != nil {
			return Summary{}, err
		}
	}
	summary.AgentCertificateRecoveryGrants = len(recoveryGrants)

	clients, err := source.ListClients(ctx)
	if err != nil {
		return Summary{}, err
	}
	for _, client := range clients {
		if err := target.PutClient(ctx, client); err != nil {
			return Summary{}, err
		}

		assignments, err := source.ListClientAssignments(ctx, client.ID)
		if err != nil {
			return Summary{}, err
		}
		for _, assignment := range assignments {
			if err := target.PutClientAssignment(ctx, assignment); err != nil {
				return Summary{}, err
			}
		}
		summary.ClientAssignments += len(assignments)

		deployments, err := source.ListClientDeployments(ctx, client.ID)
		if err != nil {
			return Summary{}, err
		}
		for _, deployment := range deployments {
			if err := target.PutClientDeployment(ctx, deployment); err != nil {
				return Summary{}, err
			}
		}
		summary.ClientDeployments += len(deployments)
	}
	summary.Clients = len(clients)

	settings, settingsErr := source.GetPanelSettings(ctx)
	if settingsErr != nil && !errors.Is(settingsErr, storage.ErrNotFound) {
		return Summary{}, settingsErr
	}
	if settingsErr == nil {
		if err := target.PutPanelSettings(ctx, settings); err != nil {
			return Summary{}, err
		}
		summary.PanelSettings = 1
	}

	authority, authorityErr := source.GetCertificateAuthority(ctx)
	if authorityErr != nil && !errors.Is(authorityErr, storage.ErrNotFound) {
		return Summary{}, authorityErr
	}
	if authorityErr == nil {
		if err := target.PutCertificateAuthority(ctx, authority); err != nil {
			return Summary{}, err
		}
	}

	if err := verifyCounts(ctx, target, summary); err != nil {
		return Summary{}, err
	}
	if authorityErr == nil {
		storedAuthority, getErr := target.GetCertificateAuthority(ctx)
		if getErr != nil {
			return Summary{}, getErr
		}
		if storedAuthority.CAPEM != authority.CAPEM || storedAuthority.PrivateKeyPEM != authority.PrivateKeyPEM || !storedAuthority.UpdatedAt.Equal(authority.UpdatedAt) {
			return Summary{}, fmt.Errorf("migration verification failed: expected persisted certificate authority to round-trip")
		}
	}

	return summary, nil
}

func ensureTargetEmpty(ctx context.Context, target storage.Store) error {
	counts, err := listCounts(ctx, target)
	if err != nil {
		return err
	}
	if counts.Users > 0 || counts.UserAppearance > 0 || counts.FleetGroups > 0 || counts.Agents > 0 || counts.Instances > 0 || counts.Jobs > 0 || counts.JobTargets > 0 || counts.AuditEvents > 0 || counts.MetricSnapshots > 0 || counts.EnrollmentTokens > 0 || counts.AgentCertificateRecoveryGrants > 0 || counts.Clients > 0 || counts.ClientAssignments > 0 || counts.ClientDeployments > 0 || counts.PanelSettings > 0 {
		return ErrTargetNotEmpty
	}
	if _, err := target.GetPanelSettings(ctx); err == nil {
		return ErrTargetNotEmpty
	} else if !errors.Is(err, storage.ErrNotFound) {
		return err
	}
	if _, err := target.GetCertificateAuthority(ctx); err == nil {
		return ErrTargetNotEmpty
	} else if !errors.Is(err, storage.ErrNotFound) {
		return err
	}

	return nil
}

func verifyCounts(ctx context.Context, target storage.Store, expected Summary) error {
	actual, err := listCounts(ctx, target)
	if err != nil {
		return err
	}
	if actual != expected {
		return fmt.Errorf("migration verification failed: expected %+v, got %+v", expected, actual)
	}

	return nil
}

func listCounts(ctx context.Context, store storage.Store) (Summary, error) {
	var summary Summary

	users, err := store.ListUsers(ctx)
	if err != nil {
		return Summary{}, err
	}
	summary.Users = len(users)

	appearances, err := store.ListUserAppearances(ctx)
	if err != nil {
		return Summary{}, err
	}
	summary.UserAppearance = len(appearances)

	fleetGroups, err := store.ListFleetGroups(ctx)
	if err != nil {
		return Summary{}, err
	}
	summary.FleetGroups = len(fleetGroups)

	agents, err := store.ListAgents(ctx)
	if err != nil {
		return Summary{}, err
	}
	summary.Agents = len(agents)

	instances, err := store.ListInstances(ctx)
	if err != nil {
		return Summary{}, err
	}
	summary.Instances = len(instances)

	jobs, err := store.ListJobs(ctx)
	if err != nil {
		return Summary{}, err
	}
	summary.Jobs = len(jobs)
	for _, job := range jobs {
		targets, err := store.ListJobTargets(ctx, job.ID)
		if err != nil {
			return Summary{}, err
		}
		summary.JobTargets += len(targets)
	}

	auditEvents, err := store.ListAuditEvents(ctx, 0)
	if err != nil {
		return Summary{}, err
	}
	summary.AuditEvents = len(auditEvents)

	metricSnapshots, err := store.ListMetricSnapshots(ctx)
	if err != nil {
		return Summary{}, err
	}
	summary.MetricSnapshots = len(metricSnapshots)

	enrollmentTokens, err := store.ListEnrollmentTokens(ctx)
	if err != nil {
		return Summary{}, err
	}
	summary.EnrollmentTokens = len(enrollmentTokens)

	recoveryGrants, err := store.ListAgentCertificateRecoveryGrants(ctx)
	if err != nil {
		return Summary{}, err
	}
	summary.AgentCertificateRecoveryGrants = len(recoveryGrants)

	clients, err := store.ListClients(ctx)
	if err != nil {
		return Summary{}, err
	}
	summary.Clients = len(clients)
	for _, client := range clients {
		assignments, err := store.ListClientAssignments(ctx, client.ID)
		if err != nil {
			return Summary{}, err
		}
		summary.ClientAssignments += len(assignments)

		deployments, err := store.ListClientDeployments(ctx, client.ID)
		if err != nil {
			return Summary{}, err
		}
		summary.ClientDeployments += len(deployments)
	}

	if _, err := store.GetPanelSettings(ctx); err == nil {
		summary.PanelSettings = 1
	} else if !errors.Is(err, storage.ErrNotFound) {
		return Summary{}, err
	}

	return summary, nil
}
