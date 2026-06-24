package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

// TestPutAgent_InsideTransact_Postgres is a regression test for the inbound
// enrollment bug (GitHub #140): Store.PutAgent unconditionally rejected
// tx-bound stores with errTxBoundStore, so every Postgres-backed agent
// enrollment failed — the flow calls tx.PutAgent from inside Transact
// (server/agent_flow.go). PutAgent must compose inside a transaction like
// every other write that goes through s.db.
func TestPutAgent_InsideTransact_Postgres(t *testing.T) {
	store := openTestStoreForUoW(t)
	ctx := context.Background()

	agent := storage.AgentRecord{
		ID:         "agent-tx-enroll",
		NodeName:   "node-tx-enroll",
		Version:    "v1.2.3",
		LastSeenAt: time.Unix(1700000000, 0).UTC(),
	}

	if err := store.Transact(ctx, func(tx storage.Store) error {
		return tx.PutAgent(ctx, agent)
	}); err != nil {
		t.Fatalf("Transact(PutAgent) error = %v", err)
	}

	// Row must be visible outside the transaction after commit.
	agents, err := store.ListAgents(ctx)
	if err != nil {
		t.Fatalf("ListAgents() error = %v", err)
	}
	for _, a := range agents {
		if a.ID == agent.ID {
			if a.NodeName != agent.NodeName {
				t.Errorf("NodeName = %q, want %q", a.NodeName, agent.NodeName)
			}
			return
		}
	}
	t.Fatalf("agent %q not persisted by tx.PutAgent", agent.ID)
}
