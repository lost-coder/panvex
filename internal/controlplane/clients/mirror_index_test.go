package clients

import (
	"context"
	"fmt"
	"testing"
)

func newMirrorTestService() *Service {
	repo := newFakeRepo()
	rs := &fakeRepoSet{clients: repo, discovered: newFakeDiscoveredRepo()}
	return NewService(ServiceConfig{
		Repo:           repo,
		DiscoveredRepo: newFakeDiscoveredRepo(),
		UoW:            newFakeUoW(rs),
	})
}

func TestMirrorResolveIDByNameFollowsMutations(t *testing.T) {
	svc := newMirrorTestService()

	agentID := "agent-1"
	fleetGroupID := "fg-1"
	mk := func(id, name string) (Client, []Assignment) {
		return Client{ID: ClientID(id), Name: name},
			[]Assignment{{TargetType: TargetTypeAgent, AgentID: agentID}}
	}

	// 1. MirrorReplaceInMemory — индекс появляется.
	c1, as1 := mk("c-1", "alice")
	svc.MirrorReplaceInMemory(c1, as1, nil)
	if got := svc.MirrorResolveIDByName(agentID, fleetGroupID, "alice"); got != "c-1" {
		t.Fatalf("resolve after MirrorReplaceInMemory = %q, want c-1", got)
	}

	// 2. Переименование через MirrorReplaceInMemory — старое имя умирает.
	c1renamed, _ := mk("c-1", "bob")
	svc.MirrorReplaceInMemory(c1renamed, as1, nil)
	if got := svc.MirrorResolveIDByName(agentID, fleetGroupID, "alice"); got != "" {
		t.Fatalf("resolve of stale name = %q, want empty", got)
	}
	if got := svc.MirrorResolveIDByName(agentID, fleetGroupID, "bob"); got != "c-1" {
		t.Fatalf("resolve of new name = %q, want c-1", got)
	}

	// 3. Fleet-group fallback сохраняется (клиент назначен на группу).
	c2 := Client{ID: ClientID("c-2"), Name: "carol"}
	as2 := []Assignment{{TargetType: TargetTypeFleetGroup, FleetGroupID: FleetGroupID(fleetGroupID)}}
	svc.MirrorReplaceInMemory(c2, as2, nil)
	if got := svc.MirrorResolveIDByName(agentID, fleetGroupID, "carol"); got != "c-2" {
		t.Fatalf("fleet-group resolve = %q, want c-2", got)
	}
	// ...но не для чужой группы.
	if got := svc.MirrorResolveIDByName(agentID, "fg-OTHER", "carol"); got != "" {
		t.Fatalf("foreign-group resolve = %q, want empty", got)
	}

	// 4. Delete — индекс чистится.
	if err := svc.Delete(context.Background(), ClientID("c-1")); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if got := svc.MirrorResolveIDByName(agentID, fleetGroupID, "bob"); got != "" {
		t.Fatalf("resolve after Delete = %q, want empty", got)
	}
}

// TestMirrorResolveIDByNameEquivalentToPureResolver checks the indexed resolver
// against the pure ResolveIDByName on shared state.
func TestMirrorResolveIDByNameEquivalentToPureResolver(t *testing.T) {
	svc := newMirrorTestService()
	agentID, fgID := "agent-1", "fg-1"
	// 50 клиентов: половина по прямому назначению, половина по группе,
	// пара дубликатов имён и один клиент без назначений.
	for i := 0; i < 50; i++ {
		id := fmt.Sprintf("c-%02d", i)
		name := fmt.Sprintf("user-%02d", i%25) // дубли имён
		var as []Assignment
		switch i % 3 {
		case 0:
			as = []Assignment{{TargetType: TargetTypeAgent, AgentID: agentID}}
		case 1:
			as = []Assignment{{TargetType: TargetTypeFleetGroup, FleetGroupID: FleetGroupID(fgID)}}
		case 2:
			as = nil // не назначен — не должен резолвиться
		}
		svc.MirrorReplaceInMemory(Client{ID: ClientID(id), Name: name}, as, nil)
	}
	// Эталонные карты — как их строила старая реализация.
	clientsByID := map[string]Client{}
	assignmentsByClient := map[string][]Assignment{}
	list, err := svc.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	for _, c := range list {
		clientsByID[string(c.ID)] = c
		as, _ := svc.MirrorAssignmentsAndDeployments(string(c.ID))
		assignmentsByClient[string(c.ID)] = as
	}
	for i := 0; i < 25; i++ {
		name := fmt.Sprintf("user-%02d", i)
		want := ResolveIDByName(clientsByID, assignmentsByClient, agentID, fgID, name)
		got := svc.MirrorResolveIDByName(agentID, fgID, name)
		// Оба недетерминированы при нескольких кандидатах — сравниваем
		// множество допустимых ответов: got обязан быть РЕЗОЛВИМЫМ
		// кандидатом тогда и только тогда, когда want непуст.
		if (want == "") != (got == "") {
			t.Fatalf("name %q: pure=%q indexed=%q — resolvability differs", name, want, got)
		}
		if got != "" {
			c := clientsByID[got]
			if c.Name != name || !assignmentMatchesAgent(assignmentsByClient[got], agentID, fgID) {
				t.Fatalf("name %q: indexed answer %q is not a valid candidate", name, got)
			}
		}
	}
}

func BenchmarkMirrorResolveIDByName(b *testing.B) {
	svc := newMirrorTestService()
	for i := 0; i < 5000; i++ {
		svc.MirrorReplaceInMemory(
			Client{ID: ClientID(fmt.Sprintf("c-%d", i)), Name: fmt.Sprintf("user-%d", i)},
			[]Assignment{{TargetType: TargetTypeAgent, AgentID: "agent-1"}}, nil)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		svc.MirrorResolveIDByName("agent-1", "fg-1", "user-2500")
	}
}
