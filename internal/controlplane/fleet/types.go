package fleet

import (
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

// Group is the domain view of a fleet group. The Service speaks this type;
// storage.FleetGroupRecord stays behind the Repository, where it belongs.
//
// The two are field-identical today, which is exactly why the boundary was
// leaking: the Service handed storage records straight out to its callers, so
// the HTTP layer ended up importing the storage package to name the type it had
// just been given. The clients domain does not do that, and the inconsistency
// was the finding (R8-D). A domain type also means the day a persistence field
// appears that the domain has no business exposing, there is somewhere to NOT
// put it.
type Group struct {
	ID          string
	Name        string
	Label       string
	Description string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func groupFromRecord(record storage.FleetGroupRecord) Group {
	return Group(record)
}

func groupToRecord(group Group) storage.FleetGroupRecord {
	return storage.FleetGroupRecord(group)
}

func groupsFromRecords(records []storage.FleetGroupRecord) []Group {
	groups := make([]Group, 0, len(records))
	for _, record := range records {
		groups = append(groups, groupFromRecord(record))
	}
	return groups
}
