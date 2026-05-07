package settings

import (
	"fmt"
	"strings"
)

// parseTag turns the body of a `setting:"…"` struct tag into a FieldMeta.
// GoField, Class, Min, Max, Values, Restart, Secret are filled from
// individual keys when present; the caller is responsible for setting
// GoField and Class (those come from where the field lives in the
// registry, not from the tag).
func parseTag(raw string) (FieldMeta, error) {
	tokens, err := tokenizeTag(raw)
	if err != nil {
		return FieldMeta{}, err
	}
	out := FieldMeta{}
	for _, tok := range tokens {
		k, v := tok.key, tok.value
		switch k {
		case "name":
			out.Name = v
		case "type":
			out.Type = Type(v)
		case "default":
			out.Default = v
			out.HasDefault = true
		case "min":
			out.Min = v
		case "max":
			out.Max = v
		case "values":
			out.Values = strings.Split(v, "|")
		case "env":
			out.Env = v
		case "toml":
			out.Toml = v
		case "secret":
			out.Secret = (v == "true")
		case "store":
			out.Store = v
		case "restart":
			out.Restart = (v == "true")
		case "desc":
			out.Desc = v
		default:
			return FieldMeta{}, fmt.Errorf("settings: unknown tag attribute %q", k)
		}
	}
	if out.Name == "" {
		return FieldMeta{}, fmt.Errorf("settings: tag missing required attribute 'name'")
	}
	if out.Type == "" {
		return FieldMeta{}, fmt.Errorf("settings: tag for %q missing required attribute 'type'", out.Name)
	}
	if out.Desc == "" {
		return FieldMeta{}, fmt.Errorf("settings: tag for %q missing required attribute 'desc'", out.Name)
	}
	return out, nil
}

type tagToken struct{ key, value string }

// tokenizeTag splits a tag body on commas that are NOT inside a single-quoted
// description. Each token is split on the first '=' into key=value.
func tokenizeTag(raw string) ([]tagToken, error) {
	var (
		toks    []tagToken
		buf     strings.Builder
		inQuote bool
	)
	flush := func() error {
		s := strings.TrimSpace(buf.String())
		buf.Reset()
		if s == "" {
			return nil
		}
		eq := strings.IndexByte(s, '=')
		if eq < 0 {
			return fmt.Errorf("settings: tag fragment %q has no '='", s)
		}
		k := strings.TrimSpace(s[:eq])
		v := strings.TrimSpace(s[eq+1:])
		// strip surrounding single quotes if present
		if len(v) >= 2 && v[0] == '\'' && v[len(v)-1] == '\'' {
			v = v[1 : len(v)-1]
		}
		toks = append(toks, tagToken{key: k, value: v})
		return nil
	}
	for i := 0; i < len(raw); i++ {
		c := raw[i]
		switch {
		case c == '\'':
			inQuote = !inQuote
			buf.WriteByte(c)
		case c == ',' && !inQuote:
			if err := flush(); err != nil {
				return nil, err
			}
		case c == '\n', c == '\t':
			buf.WriteByte(' ')
		default:
			buf.WriteByte(c)
		}
	}
	if inQuote {
		return nil, fmt.Errorf("settings: tag has unterminated single quote")
	}
	if err := flush(); err != nil {
		return nil, err
	}
	return toks, nil
}
