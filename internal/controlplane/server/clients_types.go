package server

import (
	"github.com/lost-coder/panvex/internal/controlplane/clients"
	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

// Managed-client state types and their persistence shims now live in
// internal/controlplane/clients. Server keeps thin aliases here so the
// existing call sites (HTTP handlers, agent-flow, discovery) do not
// need to be renamed in one pass.
//
// The migration runs in two phases:
//
//  1. P3-ARCH-01b (this commit): clients.Service owns the in-memory
//     store and the pure helpers; server delegates reads and persistence
//     to it. The aliases below keep source-level compatibility for the
//     remaining unconverted call sites.
//
//  2. Follow-up: rename the call sites to use clients.* directly, drop
//     the aliases, and retire the thin wrappers on Server.
const (
	clientAssignmentTargetFleetGroup = clients.TargetTypeFleetGroup
	clientAssignmentTargetAgent      = clients.TargetTypeAgent

	clientDeploymentStatusQueued    = clients.DeploymentStatusQueued
	clientDeploymentStatusSucceeded = clients.DeploymentStatusSucceeded
	clientDeploymentStatusFailed    = clients.DeploymentStatusFailed
)

type (
	managedClient           = clients.Client
	managedClientAssignment = clients.Assignment
	managedClientDeployment = clients.Deployment
)

// clientToRecord, clientFromRecord, and their siblings now live in
// the clients package. These thin wrappers keep the existing call
// sites compiling while the rename lands.
func clientToRecord(client managedClient) storage.ClientRecord {
	return clients.ClientToRecord(client)
}

func clientFromRecord(record storage.ClientRecord) managedClient {
	return clients.ClientFromRecord(record)
}

func clientAssignmentToRecord(assignment managedClientAssignment) storage.ClientAssignmentRecord {
	return clients.AssignmentToRecord(assignment)
}

func clientAssignmentFromRecord(record storage.ClientAssignmentRecord) managedClientAssignment {
	return clients.AssignmentFromRecord(record)
}

func clientDeploymentToRecord(deployment managedClientDeployment) storage.ClientDeploymentRecord {
	return clients.DeploymentToRecord(deployment)
}

func clientDeploymentFromRecord(record storage.ClientDeploymentRecord) managedClientDeployment {
	return clients.DeploymentFromRecord(record)
}
