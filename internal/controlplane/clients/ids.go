// internal/controlplane/clients/ids.go
//
// Strong-typed identifiers for the clients domain. Conversion to/from
// string is explicit so that handler-layer code cannot accidentally
// pass an agent ID where a client ID is expected.
package clients

type (
	ClientID     string
	FleetGroupID string
	AssignmentID string
	DeploymentID string
)

func (id ClientID) String() string     { return string(id) }
func (id FleetGroupID) String() string { return string(id) }
func (id AssignmentID) String() string { return string(id) }
func (id DeploymentID) String() string { return string(id) }

func (id ClientID) IsZero() bool     { return id == "" }
func (id FleetGroupID) IsZero() bool { return id == "" }
func (id AssignmentID) IsZero() bool { return id == "" }
func (id DeploymentID) IsZero() bool { return id == "" }
