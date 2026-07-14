package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeDeadLetterSpool writes a spool file with 2 valid envelopes followed by
// one torn/malformed line, mirroring what an append-during-degradation write
// can leave behind after a crash.
func writeDeadLetterSpool(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "audit-events.jsonl")
	content := strings.Join([]string{
		`{"dead_lettered_at":"2026-07-01T00:00:00Z","event":{"ID":"audit-1","ActorID":"actor-1","Action":"user.login","TargetID":"target-1"}}`,
		`{"dead_lettered_at":"2026-07-01T00:01:00Z","event":{"ID":"audit-2","ActorID":"actor-2","Action":"user.logout","TargetID":"target-2"}}`,
		`{"dead_lettered_at": totally not json`,
	}, "\n") + "\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write spool fixture: %v", err)
	}
	return path
}

func TestBuildAuditDeadletterReportTable(t *testing.T) {
	dir := t.TempDir()
	path := writeDeadLetterSpool(t, dir)

	report, err := buildAuditDeadletterReport(path, "table")
	if err != nil {
		t.Fatalf("buildAuditDeadletterReport: %v", err)
	}
	if !strings.Contains(report, "audit-1") {
		t.Errorf("report missing audit-1:\n%s", report)
	}
	if !strings.Contains(report, "audit-2") {
		t.Errorf("report missing audit-2:\n%s", report)
	}
	if !strings.Contains(report, "line 3: unparseable") {
		t.Errorf("report missing torn-line note:\n%s", report)
	}
	if !strings.Contains(report, "outside the audit hash chain") {
		t.Errorf("report missing outside-chain summary:\n%s", report)
	}
}

func TestBuildAuditDeadletterReportJSON(t *testing.T) {
	dir := t.TempDir()
	path := writeDeadLetterSpool(t, dir)

	report, err := buildAuditDeadletterReport(path, "json")
	if err != nil {
		t.Fatalf("buildAuditDeadletterReport: %v", err)
	}
	if !strings.Contains(report, `"ID":"audit-1"`) {
		t.Errorf("json report missing audit-1 envelope:\n%s", report)
	}
	if !strings.Contains(report, `"ID":"audit-2"`) {
		t.Errorf("json report missing audit-2 envelope:\n%s", report)
	}
	if !strings.Contains(report, "line 3: unparseable") {
		t.Errorf("json report missing torn-line note:\n%s", report)
	}
}

func TestBuildAuditDeadletterReportMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "does-not-exist.jsonl")

	report, err := buildAuditDeadletterReport(path, "table")
	if err != nil {
		t.Fatalf("expected nil error for missing spool, got: %v", err)
	}
	if !strings.Contains(report, "no dead-letter spool found") {
		t.Errorf("expected friendly missing-spool message, got:\n%s", report)
	}
}

func TestRunAuditDeadletterMissingFileIsNotAnError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "does-not-exist.jsonl")

	if err := runAuditDeadletter([]string{"-path", path}); err != nil {
		t.Fatalf("runAuditDeadletter with missing spool: expected nil, got %v", err)
	}
}

func TestRunAuditDeadletterRejectsUnknownFormat(t *testing.T) {
	dir := t.TempDir()
	path := writeDeadLetterSpool(t, dir)

	if err := runAuditDeadletter([]string{"-path", path, "-format", "xml"}); err == nil {
		t.Fatal("expected error for unsupported -format")
	}
}
