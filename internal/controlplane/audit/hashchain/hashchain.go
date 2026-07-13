// Package hashchain implements the audit_events tamper-evident
// chain primitives. The control-plane uses these on the producer
// side (server/audit_hash_chain.go's chainAuditRecordLocked) and
// the verifier subcommand uses them on the consumer side
// (cmd/control-plane/verify_audit_chain.go) — they previously
// lived in two copies and would drift the moment one side
// changed without the other.
//
// Migration 0038 added prev_hash + event_hash to audit_events.
// The verifier walks the table chronologically; recompute mismatch
// names the offending event id. Pre-migration rows have empty
// hashes — the verifier treats them as the chain-genesis prefix.
//
// This package is a leaf primitive: it depends only on the standard
// library so the hash domain can't shift under it (R4). Callers pass a
// Record built from their own row type rather than importing a storage
// struct here.
package hashchain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"
	"time"
)

// Record is the subset of an audit event that is covered by the hash. It is
// defined locally (not imported from storage) so hashchain stays a leaf.
type Record struct {
	ID        string
	ActorID   string
	Action    string
	TargetID  string
	CreatedAt time.Time
	Details   map[string]any
}

// ComputeEventHash returns the hex-encoded SHA-256 of
//
//	prev_hash || U+001F || canonical(record)
//
// The unit-separator byte makes the prev_hash boundary unambiguous —
// a prev_hash that happened to embed the literal payload prefix
// can't silently glue itself onto the next record.
//
// canonical(record) is a canonical-JSON object over ALL covered fields with
// keys sorted recursively. Encoding each field as a distinct JSON member
// (rather than the old "%s|%s|…" join) removes the delimiter-collision where
// (Action="a|b", TargetID="c") and (Action="a", TargetID="b|c") hashed
// identically (R4, audit 2026-07-07 §1.5). Byte-identical records serialised
// on different machines always produce the same hash.
//
// The producer side is a single batch-writer goroutine plus the
// rare appendAuditSync path serialised through metricsAuditMu — the
// chain has a single producer at a time. See server/audit_trail.go
// for the race contract.
func ComputeEventHash(prevHash string, r Record) (string, error) {
	details := r.Details
	if details == nil {
		details = map[string]any{}
	}
	canonical, err := canonicaliseJSONValue(map[string]any{
		"id":         r.ID,
		"actor_id":   r.ActorID,
		"action":     r.Action,
		"target_id":  r.TargetID,
		"created_at": r.CreatedAt.UTC().Format(time.RFC3339Nano),
		"details":    details,
	})
	if err != nil {
		return "", err
	}
	h := sha256.New()
	h.Write([]byte(prevHash))
	h.Write([]byte{0x1f}) // ASCII unit separator: prev_hash boundary marker
	h.Write([]byte(canonical))
	return hex.EncodeToString(h.Sum(nil)), nil
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
