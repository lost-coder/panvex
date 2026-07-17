// Package events pins the panel's event taxonomy as a compile-time
// contract (P3-3.3, audit #22). Every type published on the eventbus must
// be a constant from here — string literals in publishers are forbidden.
//
// SYNC: web/src/shared/events/event-types.ts — mirror TS list.
// The check is done by web/scripts/check-event-parity.mjs (CI, npm run
// check:events): it parses the const block below with the regex
// `Type\w+\s*=\s*"…"`, so the block's format must not change.
package events

const (
	TypeAgentsEnrolled      = "agents.enrolled"
	TypeAgentsUpdated       = "agents.updated"
	TypeAuditCreated        = "audit.created"
	TypeClientsUpdated      = "clients.updated"
	TypeEnrollmentEvent     = "enrollment.event"
	TypeEnrollmentCompleted = "enrollment.completed"
	TypeEnrollmentFailed    = "enrollment.failed"
	TypeJobsCreated         = "jobs.created"
	TypeRuntimeEvents       = "runtime.events"
)

// All lists every published type; used by tests and
// potentially by runtime validation. Keep it in lexicographic order.
var All = []string{
	TypeAgentsEnrolled,
	TypeAgentsUpdated,
	TypeAuditCreated,
	TypeClientsUpdated,
	TypeEnrollmentCompleted,
	TypeEnrollmentEvent,
	TypeEnrollmentFailed,
	TypeJobsCreated,
	TypeRuntimeEvents,
}
