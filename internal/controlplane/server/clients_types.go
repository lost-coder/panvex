package server

import (
	"github.com/lost-coder/panvex/internal/controlplane/clients"
)

// Q5.U-Q-02: the per-record conversion shims that lived here were
// retired. Call sites use clients.ClientToRecord / FromRecord directly.
// We keep the friendly aliases below so HTTP-layer code can keep
// reading "managedClient" / "managedClientAssignment" without a
// package-prefix tax.
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
