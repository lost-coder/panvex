package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/config"
	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

// runVerifyAuditChain walks the audit_events table chronologically and
// recomputes each row's event_hash. Any mismatch (prev_hash inconsistent
// with the previous row's event_hash, or event_hash that does not match
// the canonical SHA-256 of the row) is reported with the offending event
// ID and the current vs expected hashes. Migration 0038 introduced the
// chain; rows persisted before the migration carry empty hashes and are
// tolerated as the genesis prefix.
//
// The walk is driven by ListAuditEventsCursor (DESC) and re-ordered into
// ascending chronological order in memory. Pre-release scope: an
// operator with millions of audit rows should paginate manually; we
// document the soft limit in the help text.
func runVerifyAuditChain(args []string) error {
	flags := flag.NewFlagSet("verify-audit-chain", flag.ContinueOnError)
	storageDriver := flags.String(flagStorageDriver, "", helpStorageDriver)
	storageDSN := flags.String(flagStorageDSN, "", helpStorageDSN)
	pageSize := flags.Int("page-size", 1000, "Page size for the chronological walk")
	maxRows := flags.Int("max-rows", 200_000, "Soft cap on total rows walked (the chain is held in memory once)")
	flags.Usage = func() {
		fmt.Fprintf(flags.Output(), "Usage: panvex-control-plane verify-audit-chain [flags]\n\n")
		fmt.Fprintf(flags.Output(), "Walks audit_events chronologically and recomputes the SHA-256 chain.\n")
		fmt.Fprintf(flags.Output(), "Exits non-zero on the first mismatch (or after walking the full table\n")
		fmt.Fprintf(flags.Output(), "without finding one).\n\n")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		return err
	}

	storageConfig, err := config.ResolveStorage(*storageDriver, *storageDSN)
	if err != nil {
		return err
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	store, err := openStore(storageConfig)
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer store.Close()

	rows, err := collectAuditChainAscending(ctx, store, *pageSize, *maxRows)
	if err != nil {
		return err
	}
	report, mismatch := verifyAuditChainRows(rows)
	if _, werr := io.WriteString(os.Stdout, report); werr != nil {
		return werr
	}
	if mismatch != nil {
		return mismatch
	}
	return nil
}

// collectAuditChainAscending pages through audit_events newest-first via
// ListAuditEventsCursor and returns the rows re-sorted into ascending
// chronological order. The hard cap protects an operator from accidentally
// loading a multi-million-row table into memory.
func collectAuditChainAscending(ctx context.Context, store storage.Store, pageSize, maxRows int) ([]storage.AuditEventRecord, error) {
	if pageSize <= 0 {
		pageSize = 1000
	}
	if maxRows <= 0 {
		maxRows = 200_000
	}
	all := make([]storage.AuditEventRecord, 0, pageSize)
	cursor := storage.ListAuditEventsCursorParams{Limit: pageSize}
	for {
		page, next, err := store.ListAuditEventsCursor(ctx, cursor)
		if err != nil {
			return nil, fmt.Errorf("page audit events: %w", err)
		}
		all = append(all, page...)
		if len(all) >= maxRows {
			return nil, fmt.Errorf("audit chain exceeds --max-rows=%d; bump the limit or paginate manually", maxRows)
		}
		if next.AfterID == "" && next.AfterCreatedAt.IsZero() {
			break
		}
		cursor = next
	}
	sort.SliceStable(all, func(i, j int) bool {
		if !all[i].CreatedAt.Equal(all[j].CreatedAt) {
			return all[i].CreatedAt.Before(all[j].CreatedAt)
		}
		return all[i].ID < all[j].ID
	})
	return all, nil
}

// verifyAuditChainRows is the pure verification kernel: takes rows in
// ascending chronological order, recomputes the chain, and reports the
// first inconsistency found. The report is always non-empty so the CLI
// always prints something the operator can paste into a ticket.
func verifyAuditChainRows(rows []storage.AuditEventRecord) (report string, mismatch error) {
	var b strings.Builder
	fmt.Fprintf(&b, "audit chain verifier — %d row(s) walked\n", len(rows))

	prev := ""
	skippedGenesis := 0
	for i, row := range rows {
		// Pre-migration genesis prefix: rows persisted before
		// migration 0038 carry empty hashes. Treat the leading run
		// of empty-hash rows as an opaque legacy block and start the
		// real chain check at the first row whose EventHash != "".
		if prev == "" && row.PrevHash == "" && row.EventHash == "" {
			skippedGenesis++
			continue
		}

		expectedPrev := prev
		if row.PrevHash != expectedPrev {
			return b.String() + fmt.Sprintf(
				"BROKEN at event %s (created_at %s): prev_hash mismatch\n  stored:    %s\n  expected:  %s\n",
				row.ID, row.CreatedAt.UTC().Format(time.RFC3339Nano),
				short(row.PrevHash), short(expectedPrev),
			), errAuditChainMismatch
		}

		// Recompute the event hash to detect rewrites of the row's
		// own fields (id/actor/action/target/created_at/details).
		recomputed, err := computeAuditEventHashLocal(prev, row)
		if err != nil {
			return b.String() + fmt.Sprintf(
				"hash compute failed at event %s: %v\n",
				row.ID, err,
			), err
		}
		if !strings.EqualFold(row.EventHash, recomputed) {
			return b.String() + fmt.Sprintf(
				"BROKEN at event %s (created_at %s, position %d): event_hash mismatch\n  stored:    %s\n  computed:  %s\n",
				row.ID, row.CreatedAt.UTC().Format(time.RFC3339Nano), i,
				short(row.EventHash), short(recomputed),
			), errAuditChainMismatch
		}
		prev = row.EventHash
	}

	if skippedGenesis > 0 {
		fmt.Fprintf(&b, "  pre-migration genesis prefix: %d row(s) (empty hash, no chain)\n", skippedGenesis)
	}
	fmt.Fprintf(&b, "audit chain: consistent.\n")
	return b.String(), nil
}

// errAuditChainMismatch sentinels a verifier failure. main.go's exit-code
// glue maps any non-nil error from runVerifyAuditChain to a non-zero exit;
// this constant just lets tests assert on the cause without parsing the
// message.
var errAuditChainMismatch = errors.New("audit chain mismatch")

// short returns the first 12 chars of a hex hash for human-friendly
// reporting. Empty input becomes "(empty)" so the Markdown stays aligned.
func short(h string) string {
	if h == "" {
		return "(empty)"
	}
	if len(h) > 12 {
		return h[:12] + "…"
	}
	return h
}

// computeAuditEventHashLocal mirrors the producer-side hash function
// (server.computeAuditEventHash) but lives in the cmd/ tree so the
// verifier doesn't pull in the entire control-plane runtime. Keep the
// two implementations byte-identical — there is a contract test in
// internal/controlplane/server/audit_hash_chain_test.go that pins the
// algorithm.
func computeAuditEventHashLocal(prevHash string, r storage.AuditEventRecord) (string, error) {
	canonical, err := canonicaliseDetailsLocal(r.Details)
	if err != nil {
		return "", err
	}
	payload := fmt.Sprintf(
		"%s|%s|%s|%s|%s|%s",
		r.ID, r.ActorID, r.Action, r.TargetID,
		r.CreatedAt.UTC().Format(time.RFC3339Nano),
		canonical,
	)
	h := sha256.New()
	h.Write([]byte(prevHash))
	h.Write([]byte{0x1f})
	h.Write([]byte(payload))
	return hex.EncodeToString(h.Sum(nil)), nil
}

func canonicaliseDetailsLocal(details map[string]any) (string, error) {
	if len(details) == 0 {
		return "{}", nil
	}
	return canonicaliseJSONValueLocal(details)
}

func canonicaliseJSONValueLocal(v any) (string, error) {
	switch t := v.(type) {
	case nil:
		return "null", nil
	case bool, float64, int, int64, uint64, int32:
		b, err := json.Marshal(t)
		if err != nil {
			return "", err
		}
		return string(b), nil
	case string:
		b, err := json.Marshal(t)
		if err != nil {
			return "", err
		}
		return string(b), nil
	case json.Number:
		return t.String(), nil
	case []any:
		parts := make([]string, 0, len(t))
		for _, item := range t {
			s, err := canonicaliseJSONValueLocal(item)
			if err != nil {
				return "", err
			}
			parts = append(parts, s)
		}
		return "[" + strings.Join(parts, ",") + "]", nil
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		parts := make([]string, 0, len(t))
		for _, k := range keys {
			kEnc, err := json.Marshal(k)
			if err != nil {
				return "", err
			}
			vEnc, err := canonicaliseJSONValueLocal(t[k])
			if err != nil {
				return "", err
			}
			parts = append(parts, string(kEnc)+":"+vEnc)
		}
		return "{" + strings.Join(parts, ",") + "}", nil
	default:
		b, err := json.Marshal(t)
		if err != nil {
			return "", err
		}
		return string(b), nil
	}
}
