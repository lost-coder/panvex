// internal/controlplane/discovered/ids.go
//
// Strong-typed identifiers for the discovered domain. Conversion to/from
// string is explicit so that handler-layer code cannot accidentally
// pass an agent ID where a discovered-client ID is expected.
package discovered

type DiscoveredID string

func (id DiscoveredID) String() string { return string(id) }
func (id DiscoveredID) IsZero() bool   { return id == "" }
