package server

import (
	"github.com/lost-coder/panvex/internal/controlplane/eventbus"
	cpevents "github.com/lost-coder/panvex/internal/controlplane/events"
	"github.com/lost-coder/panvex/internal/controlplane/jobs"
)

// publishClientsUpdated revives the dead clients.* branch on the frontend
// (P3-3.3, audit #22): the client list stops being invalidated only
// alongside audit.created. Published after every successful client-state
// write (create/update/delete/redeploy/rotate/reset-quota/adopt).
func (s *Server) publishClientsUpdated(clientID any) {
	s.events.Publish(eventbus.Event{
		Type: cpevents.TypeClientsUpdated,
		Data: map[string]any{"client_id": clientID},
	})
}

// publishJobCreated emits jobs.created for EVERY created job (P3-3.3,
// audit #22): previously only the manual POST /api/jobs did. PayloadJSON is
// stripped before publish — parity with the /api/jobs redact (L-2): the WS
// bus must not become a side channel for secrets in payloads.
func (s *Server) publishJobCreated(job jobs.Job) {
	job.PayloadJSON = ""
	s.events.Publish(eventbus.Event{Type: cpevents.TypeJobsCreated, Data: job})
}
