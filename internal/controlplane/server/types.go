package server

import (
	"github.com/lost-coder/panvex/internal/controlplane/api"
	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

// The control-plane presentation types moved to internal/controlplane/api so
// domain packages (agents.LiveStore) can consume them without importing
// server. server keeps these aliases so its existing call sites — hundreds of
// references to Agent / Instance / AgentRuntime / ... — compile unchanged.
type (
	Agent                   = api.Agent
	RuntimeEvent            = api.RuntimeEvent
	RuntimeDC               = api.RuntimeDC
	RuntimeUpstream         = api.RuntimeUpstream
	ConnectionClassCount    = api.ConnectionClassCount
	AgentRuntime            = api.AgentRuntime
	RuntimeSystemLoad       = api.RuntimeSystemLoad
	RuntimeMeWritersSummary = api.RuntimeMeWritersSummary
	Instance                = api.Instance
	MetricSnapshot          = api.MetricSnapshot
	AuditEvent              = api.AuditEvent

	// Unexported: the recovery-grant view is nested in Agent but server code
	// references it by its original lowercase name.
	agentCertificateRecoveryGrantResponse = api.AgentCertificateRecoveryGrantResponse
)

// The storage <-> presentation converters stay in server: they touch
// storage.*Record, and api must not import storage (that dependency is what
// the split exists to avoid).

func agentToRecord(agent Agent) storage.AgentRecord {
	return storage.AgentRecord{
		ID:            agent.ID,
		NodeName:      agent.NodeName,
		FleetGroupID:  agent.FleetGroupID,
		Version:       agent.Version,
		ReadOnly:      agent.ReadOnly,
		LastSeenAt:    agent.LastSeenAt.UTC(),
		CertIssuedAt:  agent.CertIssuedAt,
		CertExpiresAt: agent.CertExpiresAt,
	}
}

func agentFromRecord(record storage.AgentRecord) Agent {
	return Agent{
		ID:            record.ID,
		NodeName:      record.NodeName,
		FleetGroupID:  record.FleetGroupID,
		Version:       record.Version,
		ReadOnly:      record.ReadOnly,
		LastSeenAt:    record.LastSeenAt.UTC(),
		CertIssuedAt:  record.CertIssuedAt,
		CertExpiresAt: record.CertExpiresAt,
	}
}

func instanceToRecord(instance Instance) storage.InstanceRecord {
	return storage.InstanceRecord{
		ID:                instance.ID,
		AgentID:           instance.AgentID,
		Name:              instance.Name,
		Version:           instance.Version,
		ConfigFingerprint: instance.ConfigFingerprint,
		Connections:       instance.Connections,
		ReadOnly:          instance.ReadOnly,
		UpdatedAt:         instance.UpdatedAt.UTC(),
	}
}

func instanceFromRecord(record storage.InstanceRecord) Instance {
	return Instance{
		ID:                record.ID,
		AgentID:           record.AgentID,
		Name:              record.Name,
		Version:           record.Version,
		ConfigFingerprint: record.ConfigFingerprint,
		Connections:       record.Connections,
		ReadOnly:          record.ReadOnly,
		UpdatedAt:         record.UpdatedAt.UTC(),
	}
}

func metricSnapshotToRecord(snapshot MetricSnapshot) storage.MetricSnapshotRecord {
	return storage.MetricSnapshotRecord{
		ID:         snapshot.ID,
		AgentID:    snapshot.AgentID,
		InstanceID: snapshot.InstanceID,
		CapturedAt: snapshot.CapturedAt.UTC(),
		Values:     snapshot.Values,
	}
}

func metricSnapshotFromRecord(record storage.MetricSnapshotRecord) MetricSnapshot {
	return MetricSnapshot{
		ID:         record.ID,
		AgentID:    record.AgentID,
		InstanceID: record.InstanceID,
		CapturedAt: record.CapturedAt.UTC(),
		Values:     record.Values,
	}
}

func auditEventToRecord(event AuditEvent) storage.AuditEventRecord {
	return storage.AuditEventRecord{
		ID:        event.ID,
		ActorID:   event.ActorID,
		Action:    event.Action,
		TargetID:  event.TargetID,
		CreatedAt: event.CreatedAt.UTC(),
		Details:   event.Details,
	}
}

func auditEventFromRecord(record storage.AuditEventRecord) AuditEvent {
	return AuditEvent{
		ID:        record.ID,
		ActorID:   record.ActorID,
		Action:    record.Action,
		TargetID:  record.TargetID,
		CreatedAt: record.CreatedAt.UTC(),
		Details:   normalizeAuditDetails(record.Details),
	}
}
