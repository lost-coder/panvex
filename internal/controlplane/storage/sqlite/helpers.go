package sqlite

import (
	"encoding/json"
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
