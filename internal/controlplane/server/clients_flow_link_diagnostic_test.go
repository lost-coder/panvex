// IN-M2: on a successful (non-delete) client apply, if the node returns
// no connection links, the stale links must be left in place BUT the
// deployment must carry a LinkDiagnostic so the operator is not handed a
// silently-stale link after a host/secret change. These tests pin the
// three branches applyClientJobOutcome owns on the success path.

package server

import (
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/jobs"
)

func TestApplyClientJobOutcomeLinkDiagnostic(t *testing.T) {
	now := time.Date(2026, time.May, 29, 12, 0, 0, 0, time.UTC)
	ctx := t.Context()

	t.Run("non-delete success with fresh links clears diagnostic", func(t *testing.T) {
		dep := managedClientDeployment{
			ConnectionLinks: []string{"tg://old"},
			LinkDiagnostic:  "previous stale warning",
		}
		applyClientJobOutcome(ctx, &dep, jobs.ActionClientUpdate, true, "ok",
			`{"connection_links":["tg://fresh"]}`, now)

		if got := dep.ConnectionLinks; len(got) != 1 || got[0] != "tg://fresh" {
			t.Fatalf("ConnectionLinks = %v, want [tg://fresh]", got)
		}
		if dep.LinkDiagnostic != "" {
			t.Fatalf("LinkDiagnostic = %q, want empty (fresh links clear it)", dep.LinkDiagnostic)
		}
		if dep.Status != clientDeploymentStatusSucceeded {
			t.Fatalf("Status = %q, want succeeded", dep.Status)
		}
	})

	t.Run("non-delete success with no links keeps stale links and sets diagnostic", func(t *testing.T) {
		dep := managedClientDeployment{
			ClientID:        "client-1",
			AgentID:         "agent-A",
			ConnectionLinks: []string{"tg://old"},
		}
		applyClientJobOutcome(ctx, &dep, jobs.ActionClientUpdate, true, "ok", "", now)

		if got := dep.ConnectionLinks; len(got) != 1 || got[0] != "tg://old" {
			t.Fatalf("ConnectionLinks = %v, want stale [tg://old] preserved", got)
		}
		if dep.LinkDiagnostic == "" {
			t.Fatalf("LinkDiagnostic is empty, want a stale-link warning")
		}
		if dep.Status != clientDeploymentStatusSucceeded {
			t.Fatalf("Status = %q, want succeeded", dep.Status)
		}
		if dep.LastError != "" {
			t.Fatalf("LastError = %q, want empty on success", dep.LastError)
		}
	})

	t.Run("non-delete success with empty links array sets diagnostic", func(t *testing.T) {
		dep := managedClientDeployment{
			ConnectionLinks: []string{"tg://old"},
		}
		applyClientJobOutcome(ctx, &dep, jobs.ActionClientUpdate, true, "ok",
			`{"connection_links":[]}`, now)

		if got := dep.ConnectionLinks; len(got) != 1 || got[0] != "tg://old" {
			t.Fatalf("ConnectionLinks = %v, want stale [tg://old] preserved", got)
		}
		if dep.LinkDiagnostic == "" {
			t.Fatalf("LinkDiagnostic is empty, want a stale-link warning for zero links")
		}
	})

	t.Run("delete success clears links and diagnostic", func(t *testing.T) {
		dep := managedClientDeployment{
			ConnectionLinks: []string{"tg://old"},
			LinkDiagnostic:  "previous stale warning",
		}
		applyClientJobOutcome(ctx, &dep, jobs.ActionClientDelete, true, "ok", "", now)

		if dep.ConnectionLinks != nil {
			t.Fatalf("ConnectionLinks = %v, want nil after delete", dep.ConnectionLinks)
		}
		if dep.LinkDiagnostic != "" {
			t.Fatalf("LinkDiagnostic = %q, want empty after delete", dep.LinkDiagnostic)
		}
	})

	t.Run("failure leaves diagnostic untouched", func(t *testing.T) {
		dep := managedClientDeployment{
			ConnectionLinks: []string{"tg://old"},
			LinkDiagnostic:  "earlier stale warning",
		}
		applyClientJobOutcome(ctx, &dep, jobs.ActionClientUpdate, false, "boom", "", now)

		if dep.Status != clientDeploymentStatusFailed {
			t.Fatalf("Status = %q, want failed", dep.Status)
		}
		if dep.LinkDiagnostic != "earlier stale warning" {
			t.Fatalf("LinkDiagnostic = %q, want preserved on failure", dep.LinkDiagnostic)
		}
		if dep.LastError != "boom" {
			t.Fatalf("LastError = %q, want boom", dep.LastError)
		}
	})
}
