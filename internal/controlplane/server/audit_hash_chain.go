package server

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

// chainAuditRecordLocked converts an in-memory AuditEvent into the
// storage-shape record, populating PrevHash + EventHash by chaining
// onto Server.auditChainTail and advancing the tail in the same
// critical section.
//
// Caller MUST hold metricsAuditMu — the function reads and writes
// auditChainTail without further locking. This serialises the chain
// across both the async (appendAuditWithContext) and sync
// (appendAuditSync) producer paths.
//
// On a fresh process where state_restore hasn't run (test fixtures,
// store-less servers), auditChainLoaded is false and the tail is "" —
// which is the same value the verifier treats as the chain-genesis
// sentinel. The chain begins forming on the first append.
//
// If hash computation fails (extreme: a Marshaler in Details
// returns an error), the function falls back to leaving PrevHash
// and EventHash empty rather than dropping the event. Persistence
// continues; the verifier reports the gap as legacy/genesis prefix.
// We log the failure so operators notice but do not block the audit
// path on a hashing edge case.
func (s *Server) chainAuditRecordLocked(event AuditEvent) storage.AuditEventRecord {
	record := auditEventToRecord(event)
	prev := s.auditChainTail
	hash, err := computeAuditEventHash(prev, record)
	if err != nil {
		s.logger.Error("audit chain hash compute failed",
			"event_id", record.ID,
			"action", record.Action,
			"error", err,
			"alert", "audit_chain_compute_failed",
		)
		return record
	}
	record.PrevHash = prev
	record.EventHash = hash
	s.auditChainTail = hash
	return record
}

// computeAuditEventHash returns the hex-encoded SHA-256 of
//
//	prev_hash || U+001F || canonical(record)
//
// The unit-separator byte makes the prev_hash boundary unambiguous —
// a prev_hash that happened to embed the literal payload prefix can't
// silently glue itself onto the next record. The canonical encoding
// sorts JSON object keys recursively so two byte-identical records
// serialised on different machines always produce the same hash.
//
// The producer side is the single batch-writer goroutine, plus the
// rare appendAuditSync path serialised through metricsAuditMu. The
// chain therefore has a single producer at a time — see the race
// contract documented in audit_trail.go's appendAuditWithContext.
//
// Migration 0038 added prev_hash and event_hash to audit_events. Both
// fields default to "" so rows written before the migration are
// treated as the chain-genesis prefix by the verifier.
func computeAuditEventHash(prevHash string, r storage.AuditEventRecord) (string, error) {
	canonicalDetails, err := canonicaliseDetails(r.Details)
	if err != nil {
		return "", fmt.Errorf("canonicalise audit details: %w", err)
	}
	payload := fmt.Sprintf(
		"%s|%s|%s|%s|%s|%s",
		r.ID, r.ActorID, r.Action, r.TargetID,
		r.CreatedAt.UTC().Format(time.RFC3339Nano),
		canonicalDetails,
	)
	h := sha256.New()
	h.Write([]byte(prevHash))
	h.Write([]byte{0x1f}) // ASCII unit separator: prev_hash boundary marker
	h.Write([]byte(payload))
	return hex.EncodeToString(h.Sum(nil)), nil
}

// canonicaliseDetails returns a JSON re-encoding of the details map
// with object keys sorted (recursively). An empty/nil map serialises
// as "{}" so the hash domain is stable.
func canonicaliseDetails(details map[string]any) (string, error) {
	if len(details) == 0 {
		return "{}", nil
	}
	return canonicaliseJSONValue(details)
}

// canonicaliseJSONValue walks a decoded JSON value (or a Go-native
// equivalent built from interface{} maps and slices) and emits a
// deterministic JSON string with sorted object keys. Numbers and
// strings round-trip through encoding/json so escape rules and
// floating-point representation match the standard encoder.
func canonicaliseJSONValue(v any) (string, error) {
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
		// json.Decoder with UseNumber() emits this; preserve the literal.
		return t.String(), nil
	case []any:
		parts := make([]string, 0, len(t))
		for _, item := range t {
			s, err := canonicaliseJSONValue(item)
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
			vEnc, err := canonicaliseJSONValue(t[k])
			if err != nil {
				return "", err
			}
			parts = append(parts, string(kEnc)+":"+vEnc)
		}
		return "{" + strings.Join(parts, ",") + "}", nil
	default:
		// Fall back to encoding/json's default — handles time.Time,
		// custom Marshaler types, and any concrete struct callers
		// might smuggle into Details.
		b, err := json.Marshal(t)
		if err != nil {
			return "", err
		}
		return string(b), nil
	}
}
