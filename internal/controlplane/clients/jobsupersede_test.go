package clients

import (
	"testing"

	"github.com/lost-coder/panvex/internal/controlplane/jobs"
)

// TestJobSupersedeKey pins the clients-domain supersede rule that used to be
// hardcoded inside jobs.Service (clientIDFromPayload): the four full-state
// client.* actions key by client_id; everything else — including
// client.reset_quota (a counter reset, not a desired-state upsert) — does
// not participate. Behavioural equivalence with the pre-P8.1 jobs-internal
// rule is the whole point of this table.
func TestJobSupersedeKey(t *testing.T) {
	const payload = `{"client_id":"c-1","name":"alice"}`
	cases := []struct {
		name    string
		action  jobs.Action
		payload string
		want    string
	}{
		{"client.create keys by client_id", jobs.ActionClientCreate, payload, "c-1"},
		{"client.update keys by client_id", jobs.ActionClientUpdate, payload, "c-1"},
		{"client.delete keys by client_id", jobs.ActionClientDelete, payload, "c-1"},
		{"client.rotate_secret keys by client_id", jobs.ActionClientRotateSecret, payload, "c-1"},
		{"client.reset_quota is out of scope", jobs.ActionClientResetQuota, payload, ""},
		{"non-client action is out of scope", jobs.ActionRuntimeReload, payload, ""},
		{"empty payload yields no key", jobs.ActionClientUpdate, "", ""},
		{"malformed payload yields no key", jobs.ActionClientUpdate, `{"client_id"`, ""},
		{"payload without client_id yields no key", jobs.ActionClientUpdate, `{"name":"alice"}`, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := JobSupersedeKey(tc.action, tc.payload); got != tc.want {
				t.Fatalf("JobSupersedeKey(%s, %q) = %q, want %q", tc.action, tc.payload, got, tc.want)
			}
		})
	}
}
