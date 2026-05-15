package clients

import (
	"github.com/lost-coder/panvex/internal/controlplane/secretvault"
	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

// EncryptClientRecord seals the client secret at-rest using the
// supplied vault. A nil or disabled vault is a no-op so callers can
// stay simple. An empty Secret is also passed through unchanged.
func EncryptClientRecord(record storage.ClientRecord, vault *secretvault.Vault) (storage.ClientRecord, error) {
	if vault == nil || !vault.Enabled() {
		return record, nil
	}
	if record.SecretCiphertext == "" {
		return record, nil
	}
	if secretvault.IsEncrypted(record.SecretCiphertext) {
		return record, nil
	}
	encrypted, err := vault.Encrypt(secretvault.DomainClientSecret, record.SecretCiphertext)
	if err != nil {
		return record, err
	}
	record.SecretCiphertext = encrypted
	return record, nil
}

// DecryptClientRecord reverses EncryptClientRecord. Plaintext rows from
// before the vault was enabled are returned unchanged.
func DecryptClientRecord(record storage.ClientRecord, vault *secretvault.Vault) (storage.ClientRecord, error) {
	if record.SecretCiphertext == "" {
		return record, nil
	}
	if !secretvault.IsEncrypted(record.SecretCiphertext) {
		return record, nil
	}
	decrypted, err := vault.Decrypt(secretvault.DomainClientSecret, record.SecretCiphertext)
	if err != nil {
		return record, err
	}
	record.SecretCiphertext = decrypted
	return record, nil
}

// ClientToRecord converts the in-memory Client type to its persistent
// storage.ClientRecord form. Timestamps are UTC-normalized; the
// plaintext Secret is stored in SecretCiphertext until at-rest
// encryption lands (see controlplane/server/clients_types.go comment).
func ClientToRecord(client Client) storage.ClientRecord {
	return storage.ClientRecord{
		ID:                string(client.ID),
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
		ID:                ClientID(record.ID),
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
		ID:           string(assignment.ID),
		ClientID:     string(assignment.ClientID),
		TargetType:   assignment.TargetType,
		FleetGroupID: string(assignment.FleetGroupID),
		AgentID:      assignment.AgentID,
		CreatedAt:    assignment.CreatedAt.UTC(),
	}
}

// AssignmentFromRecord reconstructs an Assignment from persistent
// storage.
func AssignmentFromRecord(record storage.ClientAssignmentRecord) Assignment {
	return Assignment{
		ID:           AssignmentID(record.ID),
		ClientID:     ClientID(record.ClientID),
		TargetType:   record.TargetType,
		FleetGroupID: FleetGroupID(record.FleetGroupID),
		AgentID:      record.AgentID,
		CreatedAt:    record.CreatedAt.UTC(),
	}
}

// DeploymentToRecord converts a Deployment to its persistent form.
func DeploymentToRecord(deployment Deployment) storage.ClientDeploymentRecord {
	return storage.ClientDeploymentRecord{
		ClientID:           string(deployment.ClientID),
		AgentID:            deployment.AgentID,
		DesiredOperation:   deployment.DesiredOperation,
		Status:             deployment.Status,
		LastError:          deployment.LastError,
		ConnectionLinks:    deployment.ConnectionLinks,
		LastAppliedAt:      deployment.LastAppliedAt,
		UpdatedAt:          deployment.UpdatedAt.UTC(),
		LastResetEpochSecs: deployment.LastResetEpochSecs,
	}
}

// DeploymentFromRecord reconstructs a Deployment from persistent
// storage.
func DeploymentFromRecord(record storage.ClientDeploymentRecord) Deployment {
	return Deployment{
		ClientID:           ClientID(record.ClientID),
		AgentID:            record.AgentID,
		DesiredOperation:   record.DesiredOperation,
		Status:             record.Status,
		LastError:          record.LastError,
		ConnectionLinks:    record.ConnectionLinks,
		LastAppliedAt:      record.LastAppliedAt,
		UpdatedAt:          record.UpdatedAt.UTC(),
		LastResetEpochSecs: record.LastResetEpochSecs,
	}
}
