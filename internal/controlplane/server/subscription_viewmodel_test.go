package server

import (
	"bytes"
	"strings"
	"testing"

	"github.com/lost-coder/panvex/internal/controlplane/subscription"
)

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

func TestSubscriptionTemplateRenders(t *testing.T) {
	v := subscriptionView{
		ClientName:       "Иван П.",
		DataQuotaBytes:   100 * 1024 * 1024 * 1024,
		TrafficUsedBytes: 18 * 1024 * 1024 * 1024,
		Nodes: []subscriptionNode{{
			NodeName: "Netherlands 1", Health: "online",
			Links: []subscription.Link{{Raw: "https://t.me/proxy?server=nl1&port=443&secret=ee01", Domain: "nl1", Port: "443", Mode: "FakeTLS"}},
		}},
	}
	var buf bytes.Buffer
	if err := subscriptionTemplate.Execute(&buf, v); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"Иван П.", "Netherlands 1", "работает", "FakeTLS", "noindex"} {
		if !strings.Contains(out, want) {
			t.Fatalf("rendered page missing %q", want)
		}
	}
}

// TestSubscriptionTemplateTgURL asserts that html/template does not sanitise
// the tg:// scheme in href attributes when using the safeURL template func.
// Without safeURL the href would be rewritten to "#ZgotmplZ".
func TestSubscriptionTemplateTgURL(t *testing.T) {
	tgLink := "tg://proxy?server=de1.example.net&port=443&secret=dd03"
	v := subscriptionView{
		ClientName: "Test",
		Nodes: []subscriptionNode{{
			NodeName: "Germany 1", Health: "online",
			Links: []subscription.Link{{Raw: tgLink, Domain: "de1.example.net", Port: "443", Mode: "Secure"}},
		}},
	}
	var buf bytes.Buffer
	if err := subscriptionTemplate.Execute(&buf, v); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	out := buf.String()
	if strings.Contains(out, "#ZgotmplZ") {
		t.Fatal("html/template sanitised the tg:// href — safeURL func is not working")
	}
	// html/template HTML-encodes '&' in attribute values (correct HTML behaviour),
	// so check for the scheme and host rather than the verbatim raw string.
	if !strings.Contains(out, `href="tg://proxy?`) {
		t.Fatalf("tg:// href scheme not found in rendered output; got snippet: %q",
			out[max(0, strings.Index(out, "href=")):min(len(out), strings.Index(out, "href=")+120)])
	}
}

func TestHumanBytes(t *testing.T) {
	tests := []struct {
		in   int64
		want string
	}{
		{0, "0 Б"},
		{512, "512 Б"},
		{1023, "1023 Б"},
		{1024, "1.0 КБ"},
		{1536, "1.5 КБ"},
		{1024 * 1024, "1.0 МБ"},
		{18 * 1024 * 1024 * 1024, "18.0 ГБ"},
		{100 * 1024 * 1024 * 1024, "100.0 ГБ"},
	}
	for _, tc := range tests {
		got := humanBytes(tc.in)
		if got != tc.want {
			t.Errorf("humanBytes(%d) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
