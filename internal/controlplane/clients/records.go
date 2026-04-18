package clients

import "github.com/lost-coder/panvex/internal/controlplane/storage"

// ClientToRecord converts the in-memory Client type to its persistent
// storage.ClientRecord form. Timestamps are UTC-normalized; the
// plaintext Secret is stored in SecretCiphertext until at-rest
// encryption lands (see controlplane/server/clients_types.go comment).
func ClientToRecord(client Client) storage.ClientRecord {
	return storage.ClientRecord{
		ID:                client.ID,
		Name:              client.Name,
		SecretCiphertext:  client.Secret,
		UserADTag:         client.UserADTag,
		Enabled:           client.Enabled,
		MaxTCPConns:       client.MaxTCPConns,
		MaxUniqueIPs:      client.MaxUniqueIPs,
		DataQuotaBytes:    client.DataQuotaBytes,
		ExpirationRFC3339: client.ExpirationRFC3339,
		CreatedAt:         client.CreatedAt.UTC(),
		UpdatedAt:         client.UpdatedAt.UTC(),
		DeletedAt:         client.DeletedAt,
	}
}

// ClientFromRecord reconstructs the in-memory Client from persistent
// storage.
func ClientFromRecord(record storage.ClientRecord) Client {
	return Client{
		ID:                record.ID,
		Name:              record.Name,
		Secret:            record.SecretCiphertext,
		UserADTag:         record.UserADTag,
		Enabled:           record.Enabled,
		MaxTCPConns:       record.MaxTCPConns,
		MaxUniqueIPs:      record.MaxUniqueIPs,
		DataQuotaBytes:    record.DataQuotaBytes,
		ExpirationRFC3339: record.ExpirationRFC3339,
		CreatedAt:         record.CreatedAt.UTC(),
		UpdatedAt:         record.UpdatedAt.UTC(),
		DeletedAt:         record.DeletedAt,
	}
}

// AssignmentToRecord converts an Assignment to its persistent form.
func AssignmentToRecord(assignment Assignment) storage.ClientAssignmentRecord {
	return storage.ClientAssignmentRecord{
		ID:           assignment.ID,
		ClientID:     assignment.ClientID,
		TargetType:   assignment.TargetType,
		FleetGroupID: assignment.FleetGroupID,
		AgentID:      assignment.AgentID,
		CreatedAt:    assignment.CreatedAt.UTC(),
	}
}

// AssignmentFromRecord reconstructs an Assignment from persistent
// storage.
func AssignmentFromRecord(record storage.ClientAssignmentRecord) Assignment {
	return Assignment{
		ID:           record.ID,
		ClientID:     record.ClientID,
		TargetType:   record.TargetType,
		FleetGroupID: record.FleetGroupID,
		AgentID:      record.AgentID,
		CreatedAt:    record.CreatedAt.UTC(),
	}
}

// DeploymentToRecord converts a Deployment to its persistent form.
func DeploymentToRecord(deployment Deployment) storage.ClientDeploymentRecord {
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

// DeploymentFromRecord reconstructs a Deployment from persistent
// storage.
func DeploymentFromRecord(record storage.ClientDeploymentRecord) Deployment {
	return Deployment{
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
