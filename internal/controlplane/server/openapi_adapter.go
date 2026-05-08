package server

import (
	"net/http"

	"github.com/lost-coder/panvex/openapi"
)

// oapiAdapter satisfies openapi.ServerInterface by delegating each
// operation to the existing per-feature handler. The OpenAPI codegen
// owns request shape, parameter decoding, and route registration; the
// hand-written handlers keep their auth/scope/middleware logic intact.
//
// Wave 3.3 (see docs/superpowers/plans/2026-05-08-api-codegen.md):
// adopt OpenAPI 3.1 as the source of truth without rewriting every
// handler in one go. Per-route auth + sensitive middleware stays where
// it always was — `routes.go` registers the wrapper handlers from
// openapi.ServerInterfaceWrapper inside the existing nested chi
// groups, so the codegen's flat `HandlerFromMux` is intentionally not
// used here.
//
// The typed string parameters that oapi-codegen passes after (w, r)
// — `id`, `value` — are ignored: the underlying handlers re-extract
// them via chi.URLParam to keep the package-internal path stable. We
// only forward the request as-is.
type oapiAdapter struct {
	s *Server
}

func newOapiAdapter(s *Server) *oapiAdapter { return &oapiAdapter{s: s} }

// Compile-time guarantee that the adapter still satisfies the
// generated interface; if codegen adds a new operation we want a
// build break here, not a 404 in production.
var _ openapi.ServerInterface = (*oapiAdapter)(nil)

func (a *oapiAdapter) GetHealthz(w http.ResponseWriter, r *http.Request) {
	handleHealthz().ServeHTTP(w, r)
}

func (a *oapiAdapter) GetVersion(w http.ResponseWriter, r *http.Request) {
	a.s.handleVersion().ServeHTTP(w, r)
}

func (a *oapiAdapter) ListAgents(w http.ResponseWriter, r *http.Request) {
	a.s.handleAgents().ServeHTTP(w, r)
}

func (a *oapiAdapter) RenameAgent(w http.ResponseWriter, r *http.Request, _ string) {
	a.s.handleRenameAgent().ServeHTTP(w, r)
}

func (a *oapiAdapter) DeregisterAgent(w http.ResponseWriter, r *http.Request, _ string) {
	a.s.handleDeregisterAgent().ServeHTTP(w, r)
}

func (a *oapiAdapter) UpdateAgentFleetGroup(w http.ResponseWriter, r *http.Request, _ string) {
	a.s.handleUpdateAgentFleetGroup().ServeHTTP(w, r)
}

func (a *oapiAdapter) UpdateAgentTransportMode(w http.ResponseWriter, r *http.Request, _ string) {
	a.s.handleUpdateAgentTransportMode().ServeHTTP(w, r)
}

func (a *oapiAdapter) DispatchAgentUpdate(w http.ResponseWriter, r *http.Request, _ string) {
	a.s.handleAgentUpdate().ServeHTTP(w, r)
}

func (a *oapiAdapter) CreateAgentInstallCommand(w http.ResponseWriter, r *http.Request, _ string) {
	a.s.handleAgentInstallCommand().ServeHTTP(w, r)
}

func (a *oapiAdapter) ListEnrollmentTokens(w http.ResponseWriter, r *http.Request) {
	a.s.handleListEnrollmentTokens().ServeHTTP(w, r)
}

func (a *oapiAdapter) CreateEnrollmentToken(w http.ResponseWriter, r *http.Request) {
	a.s.handleCreateEnrollmentToken().ServeHTTP(w, r)
}

func (a *oapiAdapter) RevokeEnrollmentToken(w http.ResponseWriter, r *http.Request, _ string) {
	a.s.handleRevokeEnrollmentToken().ServeHTTP(w, r)
}

func (a *oapiAdapter) CreateAgentCertificateRecoveryGrant(w http.ResponseWriter, r *http.Request, _ string) {
	a.s.handleCreateAgentCertificateRecoveryGrant().ServeHTTP(w, r)
}

func (a *oapiAdapter) RevokeAgentCertificateRecoveryGrant(w http.ResponseWriter, r *http.Request, _ string) {
	a.s.handleRevokeAgentCertificateRecoveryGrant().ServeHTTP(w, r)
}
