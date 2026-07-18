package postgres

import (
	"bytes"
	"encoding/json"
)

// encodeJSON / decodeJSON are shared package helpers used by the audit and
// metrics domain methods to serialise opaque JSON payloads.
func encodeJSON(value any) ([]byte, error) {
	return json.Marshal(value)
}

func decodeJSON[T any](value []byte, target *T) error {
	if len(value) == 0 {
		value = []byte("{}")
	}

	return json.Unmarshal(value, target)
}

// decodeAuditDetails decodes an audit event's details JSON with UseNumber so
// integer values survive the round-trip as their exact literal (json.Number)
// instead of being coerced to float64. The audit hash verifier re-hashes
// Details (hashchain.canonicaliseJSONValue handles json.Number), while the
// producer hashes native ints as exact decimals — coercing to float64 here
// would make any integer >= 2^53 recompute to a different hash and false-flag
// the tamper chain as broken.
func decodeAuditDetails(value []byte, target *map[string]any) error {
	if len(value) == 0 {
		value = []byte("{}")
	}
	dec := json.NewDecoder(bytes.NewReader(value))
	dec.UseNumber()
	return dec.Decode(target)
}
