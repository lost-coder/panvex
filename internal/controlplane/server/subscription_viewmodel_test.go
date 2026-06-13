package server

import "testing"

func TestGroupDeploymentLinks(t *testing.T) {
	linksByAgent := map[string][]string{
		"agent-nl": {
			"https://t.me/proxy?server=nl1.cdn.com&port=443&secret=ee01",
			"https://t.me/proxy?server=nl2.cdn.com&port=8443&secret=ee02",
		},
		"agent-de":      {"tg://proxy?server=de1.host.net&port=443&secret=dd03"},
		"agent-nolinks": {"garbage"},
	}
	nodes := map[string]nodeInfo{
		"agent-nl": {NodeName: "Netherlands 1", Health: "online"},
		"agent-de": {NodeName: "Germany 1", Health: "degraded"},
	}
	got := groupDeploymentLinks(linksByAgent, nodes)
	if len(got) != 2 {
		t.Fatalf("nodes = %d, want 2 (agent-nolinks dropped)", len(got))
	}
	if got[0].NodeName != "Germany 1" || got[0].Health != "degraded" || len(got[0].Links) != 1 {
		t.Fatalf("node[0] = %+v", got[0])
	}
	if got[1].NodeName != "Netherlands 1" || len(got[1].Links) != 2 {
		t.Fatalf("node[1] = %+v", got[1])
	}
	if got[1].Links[0].Mode != "FakeTLS" {
		t.Fatalf("link mode = %q, want FakeTLS", got[1].Links[0].Mode)
	}
}
