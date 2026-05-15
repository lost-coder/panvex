package runtime

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/agent/telemt"
	"github.com/lost-coder/panvex/internal/gatewayrpc"
)

// makeResetQuotaJob builds a JobCommand carrying the canonical
// client.reset_quota payload. Shared across the handler tests so each
// scenario only needs to set the fields that matter for the assertion.
func makeResetQuotaJob(t *testing.T, clientID, name string) *gatewayrpc.JobCommand {
	t.Helper()
	payload, err := json.Marshal(clientResetQuotaJobPayload{ClientID: clientID, Name: name})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	return &gatewayrpc.JobCommand{
		Id:          "job-reset-1",
		Action:      jobActionResetQuota,
		PayloadJson: string(payload),
	}
}

// TestResetQuotaHappyPath asserts the agent translates a successful
// Telemt response into success=true with a typed ResultJSON carrying
// used_bytes + last_reset_epoch_secs. The panel uses both values to
// drive immediate "Last reset" UI without waiting for the next snapshot.
func TestResetQuotaHappyPath(t *testing.T) {
	stub := &fakeTelemtClient{
		resetQuotaResult: telemt.ResetUserQuotaResult{
			Username:           "alice",
			UsedBytes:          0,
			LastResetEpochSecs: 1747332000,
		},
	}
	agent := New(Config{AgentID: "agent-1"}, stub)
	job := makeResetQuotaJob(t, "client-1", "alice")
	result := runResetQuotaJob(t, agent,job)

	if !result.GetSuccess() {
		t.Fatalf("expected success, got message=%q", result.GetMessage())
	}
	if stub.resetQuotaUsername != "alice" {
		t.Fatalf("resetQuotaUsername = %q, want %q", stub.resetQuotaUsername, "alice")
	}
	var payload clientResetQuotaJobResult
	if err := json.Unmarshal([]byte(result.GetResultJson()), &payload); err != nil {
		t.Fatalf("unmarshal result_json: %v", err)
	}
	if payload.LastResetEpochSecs != 1747332000 {
		t.Fatalf("LastResetEpochSecs = %d, want 1747332000", payload.LastResetEpochSecs)
	}
	if payload.UnsupportedTelemt || payload.ReadOnlyTelemt {
		t.Fatalf("flags should be off on success, got %+v", payload)
	}
}

// TestResetQuotaUnsupportedFlagsTypedReason verifies that an
// ErrResetQuotaUnsupported from Telemt surfaces as a failure with
// UnsupportedTelemt=true in ResultJSON. The panel relies on this flag
// to render "Reset unavailable (Telemt < 3.4.6)" per-deployment instead
// of a generic transport-failure message.
func TestResetQuotaUnsupportedFlagsTypedReason(t *testing.T) {
	stub := &fakeTelemtClient{resetQuotaErr: telemt.ErrResetQuotaUnsupported}
	agent := New(Config{AgentID: "agent-1"}, stub)
	result := runResetQuotaJob(t, agent,makeResetQuotaJob(t, "client-1", "alice"))

	if result.GetSuccess() {
		t.Fatal("expected success=false when endpoint is unsupported")
	}
	var payload clientResetQuotaJobResult
	if err := json.Unmarshal([]byte(result.GetResultJson()), &payload); err != nil {
		t.Fatalf("unmarshal result_json: %v", err)
	}
	if !payload.UnsupportedTelemt {
		t.Fatalf("UnsupportedTelemt = false, want true; payload=%+v", payload)
	}
	if payload.ReadOnlyTelemt {
		t.Fatalf("ReadOnlyTelemt should not be set, payload=%+v", payload)
	}
}

// TestResetQuotaReadOnlyFlagsTypedReason is the read-only twin of the
// unsupported test. Telemt returns 403 when its API is in read-only
// mode; ErrResetQuotaReadOnly surfaces as ReadOnlyTelemt=true so the
// UI can suggest the operator lift read-only rather than chase a
// transport problem.
func TestResetQuotaReadOnlyFlagsTypedReason(t *testing.T) {
	stub := &fakeTelemtClient{resetQuotaErr: telemt.ErrResetQuotaReadOnly}
	agent := New(Config{AgentID: "agent-1"}, stub)
	result := runResetQuotaJob(t, agent,makeResetQuotaJob(t, "client-1", "alice"))

	if result.GetSuccess() {
		t.Fatal("expected success=false when Telemt is read-only")
	}
	var payload clientResetQuotaJobResult
	if err := json.Unmarshal([]byte(result.GetResultJson()), &payload); err != nil {
		t.Fatalf("unmarshal result_json: %v", err)
	}
	if !payload.ReadOnlyTelemt {
		t.Fatalf("ReadOnlyTelemt = false, want true; payload=%+v", payload)
	}
}

// TestResetQuotaGenericFailureSkipsTypedReason guards against the
// handler over-eagerly tagging a network glitch as unsupported. Generic
// errors keep ResultJSON empty so the panel routes them to retry /
// observability paths instead of "endpoint not available".
func TestResetQuotaGenericFailureSkipsTypedReason(t *testing.T) {
	stub := &fakeTelemtClient{resetQuotaErr: errDummy}
	agent := New(Config{AgentID: "agent-1"}, stub)
	result := runResetQuotaJob(t, agent,makeResetQuotaJob(t, "client-1", "alice"))

	if result.GetSuccess() {
		t.Fatal("expected success=false on generic failure")
	}
	if strings.TrimSpace(result.GetResultJson()) != "" {
		t.Fatalf("ResultJson should stay empty on generic failure, got %q", result.GetResultJson())
	}
}

// TestResetQuotaInvalidPayloadRejects asserts an obviously broken
// payload (empty name) is rejected without calling Telemt — guards
// against accidental mass-reset on the empty username when the panel
// payload is malformed.
func TestResetQuotaInvalidPayloadRejects(t *testing.T) {
	stub := &fakeTelemtClient{}
	agent := New(Config{AgentID: "agent-1"}, stub)
	job := makeResetQuotaJob(t, "client-1", "")
	result := runResetQuotaJob(t, agent,job)

	if result.GetSuccess() {
		t.Fatal("expected success=false on empty name")
	}
	if stub.resetQuotaCalls != 0 {
		t.Fatalf("Telemt was called %d time(s) with empty name; want 0", stub.resetQuotaCalls)
	}
}

var errDummy = telemtDummyError("boom")

type telemtDummyError string

func (e telemtDummyError) Error() string { return string(e) }

// runResetQuotaJob is a thin wrapper that picks up a fixed observedAt
// (the timestamp matters only for findCompletedJobResult dedup, not
// for any reset-quota assertion) and keeps the call sites readable.
func runResetQuotaJob(t *testing.T, agent *Agent, job *gatewayrpc.JobCommand) *gatewayrpc.JobResult {
	t.Helper()
	return agent.HandleJob(t.Context(), job, time.Now())
}
