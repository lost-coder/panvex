package postgres

import "encoding/json"

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
