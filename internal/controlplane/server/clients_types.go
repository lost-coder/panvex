package server

import (
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

const (
	clientAssignmentTargetFleetGroup  = "fleet_group"
	clientAssignmentTargetAgent       = "agent"

	clientDeploymentStatusQueued    = "queued"
	clientDeploymentStatusSucceeded = "succeeded"
	clientDeploymentStatusFailed    = "failed"
)

type managedClient struct {
	ID                string
	Name              string
	Secret            string
	UserADTag         string
	Enabled           bool
	MaxTCPConns       int
	MaxUniqueIPs      int
	DataQuotaBytes    int64
	ExpirationRFC3339 string
	CreatedAt         time.Time
	UpdatedAt         time.Time
	DeletedAt         *time.Time
}

type managedClientAssignment struct {
	ID           string
	ClientID     string
	TargetType   string
	FleetGroupID string
	AgentID      string
	CreatedAt    time.Time
}

type managedClientDeployment struct {
	ClientID         string
	AgentID          string
	DesiredOperation string
	Status           string
	LastError        string
	ConnectionLink   string
	LastAppliedAt    *time.Time
	UpdatedAt        time.Time
}

func clientToRecord(client managedClient) storage.ClientRecord {
	return storage.ClientRecord{
		ID:               client.ID,
		Name:             client.Name,
		// SecretCiphertext temporarily stores plaintext until at-rest encryption lands.
		SecretCiphertext: client.Secret,
		UserADTag:        client.UserADTag,
		Enabled:          client.Enabled,
		MaxTCPConns:      client.MaxTCPConns,
		MaxUniqueIPs:     client.MaxUniqueIPs,
		DataQuotaBytes:   client.DataQuotaBytes,
		ExpirationRFC3339: client.ExpirationRFC3339,
		CreatedAt:        client.CreatedAt.UTC(),
		UpdatedAt:        client.UpdatedAt.UTC(),
		DeletedAt:        client.DeletedAt,
	}
}

func clientFromRecord(record storage.ClientRecord) managedClient {
	return managedClient{
		ID:               record.ID,
		Name:             record.Name,
		Secret:           record.SecretCiphertext,
		UserADTag:        record.UserADTag,
		Enabled:          record.Enabled,
		MaxTCPConns:      record.MaxTCPConns,
		MaxUniqueIPs:     record.MaxUniqueIPs,
		DataQuotaBytes:   record.DataQuotaBytes,
		ExpirationRFC3339: record.ExpirationRFC3339,
		CreatedAt:        record.CreatedAt.UTC(),
		UpdatedAt:        record.UpdatedAt.UTC(),
		DeletedAt:        record.DeletedAt,
	}
}

func clientAssignmentToRecord(assignment managedClientAssignment) storage.ClientAssignmentRecord {
	return storage.ClientAssignmentRecord{
		ID:           assignment.ID,
		ClientID:     assignment.ClientID,
		TargetType:   assignment.TargetType,
		FleetGroupID: assignment.FleetGroupID,
		AgentID:      assignment.AgentID,
		CreatedAt:    assignment.CreatedAt.UTC(),
	}
}

func clientAssignmentFromRecord(record storage.ClientAssignmentRecord) managedClientAssignment {
	return managedClientAssignment{
		ID:           record.ID,
		ClientID:     record.ClientID,
		TargetType:   record.TargetType,
		FleetGroupID: record.FleetGroupID,
		AgentID:      record.AgentID,
		CreatedAt:    record.CreatedAt.UTC(),
	}
}

func clientDeploymentToRecord(deployment managedClientDeployment) storage.ClientDeploymentRecord {
	return storage.ClientDeploymentRecord{
		ClientID:         deployment.ClientID,
		AgentID:          deployment.AgentID,
		DesiredOperation: deployment.DesiredOperation,
		Status:           deployment.Status,
		LastError:        deployment.LastError,
		ConnectionLink:   deployment.ConnectionLink,
		LastAppliedAt:    deployment.LastAppliedAt,
		UpdatedAt:        deployment.UpdatedAt.UTC(),
	}
}

func clientDeploymentFromRecord(record storage.ClientDeploymentRecord) managedClientDeployment {
	return managedClientDeployment{
		ClientID:         record.ClientID,
		AgentID:          record.AgentID,
		DesiredOperation: record.DesiredOperation,
		Status:           record.Status,
		LastError:        record.LastError,
		ConnectionLink:   record.ConnectionLink,
		LastAppliedAt:    record.LastAppliedAt,
		UpdatedAt:        record.UpdatedAt.UTC(),
	}
}
