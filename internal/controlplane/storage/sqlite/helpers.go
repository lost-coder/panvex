package sqlite

import (
	"encoding/json"
	"strings"
	"time"
)

func toUnix(value time.Time) int64 {
	return value.UTC().Unix()
}

func fromUnix(value int64) time.Time {
	return time.Unix(value, 0).UTC()
}

func boolToInt(value bool) int {
	if value {
		return 1
	}

	return 0
}

func intToBool(value int) bool {
	return value != 0
}

func encodeJSON(value any) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", err
	}

	return string(data), nil
}

func decodeJSON[T any](value string, target *T) error {
	if value == "" {
		value = "{}"
	}

	return json.Unmarshal([]byte(value), target)
}

// decodeAuditDetails decodes an audit event's details JSON with UseNumber so
// integer values survive the round-trip as their exact literal (json.Number)
// instead of being coerced to float64. The audit hash verifier re-hashes
// Details (hashchain.canonicaliseJSONValue handles json.Number), while the
// producer hashes native ints as exact decimals — coercing to float64 here
// would make any integer >= 2^53 recompute to a different hash and false-flag
// the tamper chain as broken.
func decodeAuditDetails(value string, target *map[string]any) error {
	if value == "" {
		value = "{}"
	}
	dec := json.NewDecoder(strings.NewReader(value))
	dec.UseNumber()
	return dec.Decode(target)
}
