package settings

import (
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Validate parses raw into the value type declared by the field meta and
// applies any min/max/values/regex bounds. Returns the typed value on
// success.
func Validate(f FieldMeta, raw string) (any, error) {
	switch f.Type {
	case TypeInt:
		return validateInt(f, raw)
	case TypeDuration:
		return validateDuration(f, raw)
	case TypeString:
		return validateString(f, raw)
	case TypeBool:
		return validateBool(f, raw)
	case TypeHostPort:
		return validateHostPort(f, raw)
	case TypeURL:
		return validateURL(f, raw)
	case TypeEnum:
		return validateEnum(f, raw)
	case TypeJSON:
		return validateJSON(f, raw)
	}
	return nil, fmt.Errorf("settings: %s: validator for type %q not implemented", f.Name, f.Type)
}

func validateInt(f FieldMeta, raw string) (int64, error) {
	n, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("settings: %s: not a valid integer: %w", f.Name, err)
	}
	if f.Min != "" {
		min, _ := strconv.ParseInt(f.Min, 10, 64)
		if n < min {
			return 0, fmt.Errorf("settings: %s: value %d below min %d", f.Name, n, min)
		}
	}
	if f.Max != "" {
		max, _ := strconv.ParseInt(f.Max, 10, 64)
		if n > max {
			return 0, fmt.Errorf("settings: %s: value %d above max %d", f.Name, n, max)
		}
	}
	return n, nil
}

func validateDuration(f FieldMeta, raw string) (time.Duration, error) {
	d, err := time.ParseDuration(strings.TrimSpace(raw))
	if err != nil {
		return 0, fmt.Errorf("settings: %s: not a valid duration: %w", f.Name, err)
	}
	if f.Min != "" {
		minD, perr := time.ParseDuration(f.Min)
		if perr != nil {
			return 0, fmt.Errorf("settings: %s: bad min %q: %w", f.Name, f.Min, perr)
		}
		if d < minD {
			return 0, fmt.Errorf("settings: %s: %s below min %s", f.Name, d, minD)
		}
	}
	if f.Max != "" {
		maxD, perr := time.ParseDuration(f.Max)
		if perr != nil {
			return 0, fmt.Errorf("settings: %s: bad max %q: %w", f.Name, f.Max, perr)
		}
		if d > maxD {
			return 0, fmt.Errorf("settings: %s: %s above max %s", f.Name, d, maxD)
		}
	}
	return d, nil
}

func validateString(f FieldMeta, raw string) (string, error) {
	if f.Min != "" {
		minLen, _ := strconv.Atoi(f.Min)
		if len(raw) < minLen {
			return "", fmt.Errorf("settings: %s: length %d below min %d", f.Name, len(raw), minLen)
		}
	}
	if f.Max != "" {
		maxLen, _ := strconv.Atoi(f.Max)
		if len(raw) > maxLen {
			return "", fmt.Errorf("settings: %s: length %d above max %d", f.Name, len(raw), maxLen)
		}
	}
	return raw, nil
}

func validateBool(f FieldMeta, raw string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "true", "1", "yes":
		return true, nil
	case "false", "0", "no":
		return false, nil
	}
	return false, fmt.Errorf("settings: %s: not a valid bool: %q", f.Name, raw)
}

func validateHostPort(f FieldMeta, raw string) (string, error) {
	host, port, err := net.SplitHostPort(raw)
	if err != nil {
		return "", fmt.Errorf("settings: %s: invalid host:port %q: %w", f.Name, raw, err)
	}
	if _, err := strconv.Atoi(port); err != nil {
		return "", fmt.Errorf("settings: %s: port %q is not numeric", f.Name, port)
	}
	_ = host
	return raw, nil
}

func validateURL(f FieldMeta, raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("settings: %s: invalid url: %w", f.Name, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("settings: %s: url scheme must be http or https, got %q", f.Name, u.Scheme)
	}
	if u.Host == "" {
		return nil, fmt.Errorf("settings: %s: url has empty host", f.Name)
	}
	return u, nil
}

func validateEnum(f FieldMeta, raw string) (string, error) {
	for _, v := range f.Values {
		if v == raw {
			return raw, nil
		}
	}
	return "", fmt.Errorf("settings: %s: %q not in {%s}", f.Name, raw, strings.Join(f.Values, "|"))
}

func validateJSON(f FieldMeta, raw string) (json.RawMessage, error) {
	var probe any
	if err := json.Unmarshal([]byte(raw), &probe); err != nil {
		return nil, fmt.Errorf("settings: %s: invalid json: %w", f.Name, err)
	}
	return json.RawMessage(raw), nil
}
