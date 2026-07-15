package runtime

import (
	"context"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/agent/telemt"
	"github.com/lost-coder/panvex/internal/gatewayrpc"
)

// Contract-audit F6: a redelivered client.create (panel retry after a lost
// ack) hits Telemt's 409 user_exists. The payload is a full desired-state
// upsert, so the agent must converge via the PATCH path — symmetric to the
// existing update→create (404) and delete-absent tolerances — instead of
// failing forever.
func TestAgentHandleJobCreateFallsBackToUpdateOnUserExists(t *testing.T) {
	client := &fakeTelemtClient{
		createErr:    telemt.ErrClientAlreadyExists,
		updateResult: telemt.ClientApplyResult{ConnectionLinks: []string{"tg://proxy?server=node-a&secret=converged"}},
	}
	agent := New(Config{AgentID: "agent-1", NodeName: "node-a"}, client)

	result := agent.HandleJob(context.Background(), &gatewayrpc.JobCommand{
		Id:          "job-create-redelivered",
		Action:      "client.create",
		PayloadJson: `{"client_id":"client-1","name":"alice","secret":"secret-1","enabled":true}`,
	}, time.Date(2026, time.May, 29, 13, 0, 0, 0, time.UTC))

	if !result.Success {
		t.Fatalf("HandleJob() Success = false, want true (409 user_exists must converge), message = %q", result.Message)
	}
	if client.createCalls != 1 || client.updateCalls != 1 {
		t.Fatalf("calls create=%d update=%d, want 1/1 (409 user_exists → PATCH fallback)", client.createCalls, client.updateCalls)
	}
}

// A non-user_exists failure on create must still fail the job.
func TestAgentHandleJobCreateOtherErrorStillFails(t *testing.T) {
	client := &fakeTelemtClient{
		createErr: context.DeadlineExceeded,
	}
	agent := New(Config{AgentID: "agent-1", NodeName: "node-a"}, client)

	result := agent.HandleJob(context.Background(), &gatewayrpc.JobCommand{
		Id:          "job-create-fails",
		Action:      "client.create",
		PayloadJson: `{"client_id":"client-1","name":"alice","secret":"secret-1","enabled":true}`,
	}, time.Date(2026, time.May, 29, 13, 1, 0, 0, time.UTC))

	if result.Success {
		t.Fatal("HandleJob() Success = true for a non-conflict create failure, want false")
	}
	if client.updateCalls != 0 {
		t.Fatalf("updateCalls = %d, want 0 (no fallback on non-conflict errors)", client.updateCalls)
	}
}
