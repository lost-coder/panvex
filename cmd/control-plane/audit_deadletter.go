package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/batchwriter"
)

// defaultAuditDeadLetterPath mirrors batchwriter's default spool location
// (defaultAuditDeadLetterDir + auditDeadLetterFileName), spelled out here
// since those constants are unexported in the batchwriter package.
const defaultAuditDeadLetterPath = "data/audit-deadletter/audit-events.jsonl"

// runAuditDeadletter reads the audit dead-letter spool and prints it for
// manual triage. The spool holds audit events that permanently failed to
// reach the store; they are OUTSIDE the hash chain that verify-audit-chain
// checks, and there is deliberately no auto-import: sequence IDs restart
// from the persisted maximum on boot, so replaying a spooled event would
// collide with an already-issued ID and break chain continuity. See
// deploy/audit-deadletter.md for the operator runbook.
func runAuditDeadletter(args []string) error {
	fs := flag.NewFlagSet("audit-deadletter", flag.ContinueOnError)
	path := fs.String("path", defaultAuditDeadLetterPath, "Dead-letter spool file to read")
	format := fs.String("format", "table", "Output format: table|json")
	fs.Usage = func() {
		_, _ = fmt.Fprintf(fs.Output(), "Usage: panvex-control-plane audit-deadletter [flags]\n\n")
		_, _ = fmt.Fprintf(fs.Output(), "Reads the audit dead-letter spool (events that permanently failed\n")
		_, _ = fmt.Fprintf(fs.Output(), "to reach the store) for manual triage. These records are OUTSIDE\n")
		_, _ = fmt.Fprintf(fs.Output(), "the audit hash chain checked by verify-audit-chain; there is no\n")
		_, _ = fmt.Fprintf(fs.Output(), "auto-import. See deploy/audit-deadletter.md.\n\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *format != "table" && *format != "json" {
		return fmt.Errorf("unsupported -format %q: want table|json", *format)
	}

	report, err := buildAuditDeadletterReport(*path, *format)
	if err != nil {
		return err
	}
	_, err = fmt.Print(report)
	return err
}

// buildAuditDeadletterReport reads the dead-letter spool at path and renders
// it as either a table or line-delimited JSON. A missing spool file is not
// treated as an error — it means nothing was ever dead-lettered — and yields
// a friendly one-line report instead.
func buildAuditDeadletterReport(path, format string) (string, error) {
	f, err := os.Open(path) //nolint:gosec // operator-supplied CLI flag, not user input from a request
	if err != nil {
		if os.IsNotExist(err) {
			return "no dead-letter spool found — nothing was ever dead-lettered\n", nil
		}
		return "", fmt.Errorf("open dead-letter spool %q: %w", path, err)
	}
	defer f.Close()

	events, notes := scanDeadLetterSpool(f)
	if format == "json" {
		return renderAuditDeadletterJSON(events, notes)
	}
	return renderAuditDeadletterTable(events, notes), nil
}

// scanDeadLetterSpool reads one JSON envelope per line. The buffer is raised
// beyond bufio.Scanner's 64KiB default because audit Details payloads can be
// long. A malformed line is recorded as a note and skipped rather than
// aborting the read: the spool is appended to during degradation, so a torn
// last line (e.g. a crash mid-write) is expected, not exceptional.
func scanDeadLetterSpool(f *os.File) (events []batchwriter.DeadLetteredAuditEvent, notes []string) {
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)

	line := 0
	for scanner.Scan() {
		line++
		text := strings.TrimSpace(scanner.Text())
		if text == "" {
			continue
		}
		var ev batchwriter.DeadLetteredAuditEvent
		if err := json.Unmarshal([]byte(text), &ev); err != nil {
			notes = append(notes, fmt.Sprintf("line %d: unparseable: %v", line, err))
			continue
		}
		events = append(events, ev)
	}
	if err := scanner.Err(); err != nil {
		notes = append(notes, fmt.Sprintf("scan error: %v", err))
	}
	return events, notes
}

// renderAuditDeadletterTable renders the parsed envelopes as a fixed-width
// table followed by a summary line, reiterating that these records sit
// outside the hash chain and are never auto-imported.
func renderAuditDeadletterTable(events []batchwriter.DeadLetteredAuditEvent, notes []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%-25s  %-36s  %-24s  %-36s  %s\n",
		"DEAD-LETTERED-AT", "ID", "ACTION", "ACTOR", "SUBJECT")
	for _, ev := range events {
		fmt.Fprintf(&b, "%-25s  %-36s  %-24s  %-36s  %s\n",
			ev.DeadLetteredAt.UTC().Format(time.RFC3339),
			ev.Event.ID, ev.Event.Action, ev.Event.ActorID, ev.Event.TargetID)
	}
	for _, n := range notes {
		fmt.Fprintln(&b, n)
	}
	fmt.Fprintf(&b, "%d event(s) (outside the audit hash chain; import is manual by design)\n", len(events))
	return b.String()
}

// renderAuditDeadletterJSON re-emits the parsed envelopes one per line so the
// output stays machine-readable; parser notes are appended as plain-text
// lines after the JSON so `jq` pipelines can still be pointed at this output
// (a caller filtering for lines starting with `{` gets a clean stream).
func renderAuditDeadletterJSON(events []batchwriter.DeadLetteredAuditEvent, notes []string) (string, error) {
	var b strings.Builder
	for _, ev := range events {
		line, err := json.Marshal(ev)
		if err != nil {
			return "", fmt.Errorf("marshal dead-letter event %q: %w", ev.Event.ID, err)
		}
		b.Write(line)
		b.WriteByte('\n')
	}
	for _, n := range notes {
		fmt.Fprintln(&b, n)
	}
	return b.String(), nil
}
