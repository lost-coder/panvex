// Package events закрепляет событийную таксономию панели как compile-time
// контракт (P3-3.3, аудит #22). Каждый публикуемый на eventbus тип обязан
// быть константой отсюда — строковые литералы в публикаторах запрещены.
//
// SYNC: web/src/shared/events/event-types.ts — зеркальный TS-список.
// Сверку делает web/scripts/check-event-parity.mjs (CI, npm run
// check:events): он парсит const-блок ниже регэкспом
// `Type\w+\s*=\s*"…"`, поэтому формат блока менять нельзя.
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

// All перечисляет каждый публикуемый тип; используется тестами и
// потенциально рантайм-валидацией. Держи в лексикографическом порядке.
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
