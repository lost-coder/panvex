package settings

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

// StoreReader is the minimum surface OperationalStore needs from
// persistence. The control-plane wires this onto its sqlc-generated
// store; tests pass a hand-rolled fake.
type StoreReader interface {
	ReadPanelColumn(ctx context.Context, col string) (raw string, err error)
	ReadRuntimeSetting(ctx context.Context, name string) (valueJSON string, updatedAt time.Time, updatedBy string, err error)
}

// StoreWriter is the write side; populated in a later task.
type StoreWriter interface {
	WritePanelColumn(ctx context.Context, col, raw string, who string) error
	WriteRuntimeSetting(ctx context.Context, name, valueJSON, who string) error
}

// snapshot is the immutable view returned by getters.
type snapshot struct {
	values    map[string]string // setting name -> raw scalar value (JSON for json type)
	updatedAt map[string]time.Time
	updatedBy map[string]string
}

// OperationalStore exposes typed getters and (later) batch writers
// over the operational registry.
type OperationalStore struct {
	reader StoreReader
	writer StoreWriter // optional; used by Phase 7 PUT handler

	cache atomic.Pointer[snapshot]
}

// NewOperationalStore wraps a reader; pass NewOperationalStoreRW when
// writes are also needed.
func NewOperationalStore(r StoreReader) *OperationalStore {
	s := &OperationalStore{reader: r}
	s.cache.Store(&snapshot{values: map[string]string{}})
	return s
}

// NewOperationalStoreRW wraps both reader and writer.
func NewOperationalStoreRW(r StoreReader, w StoreWriter) *OperationalStore {
	s := NewOperationalStore(r)
	s.writer = w
	return s
}

// Reload rebuilds the in-memory snapshot from the underlying store.
func (s *OperationalStore) Reload(ctx context.Context) error {
	values := map[string]string{}
	updated := map[string]time.Time{}
	by := map[string]string{}

	for _, f := range AllFields() {
		if f.Class != ClassOperational {
			continue
		}
		raw, ok, err := s.read(ctx, f)
		if err != nil {
			return fmt.Errorf("settings: reload %s: %w", f.Name, err)
		}
		if !ok && f.HasDefault {
			values[f.Name] = f.Default
			continue
		}
		if !ok {
			values[f.Name] = ""
			continue
		}
		values[f.Name] = raw
	}
	s.cache.Store(&snapshot{values: values, updatedAt: updated, updatedBy: by})
	return nil
}

func (s *OperationalStore) read(ctx context.Context, f FieldMeta) (string, bool, error) {
	if strings.HasPrefix(f.Store, "panel_settings.") {
		col := strings.TrimPrefix(f.Store, "panel_settings.")
		raw, err := s.reader.ReadPanelColumn(ctx, col)
		if err != nil {
			return "", false, nil // not-set is not fatal at load time
		}
		return raw, raw != "", nil
	}
	if f.Store == "runtime_settings" {
		jsonText, _, _, err := s.reader.ReadRuntimeSetting(ctx, f.Name)
		if err != nil {
			return "", false, nil
		}
		decoded, err := decodeRuntimeJSON(f.Type, jsonText)
		if err != nil {
			return "", false, err
		}
		return decoded, true, nil
	}
	return "", false, fmt.Errorf("settings: unknown store %q on %s", f.Store, f.Name)
}

func decodeRuntimeJSON(t Type, body string) (string, error) {
	body = strings.TrimSpace(body)
	switch t {
	case TypeString, TypeEnum, TypeURL, TypeHostPort:
		var s string
		if err := json.Unmarshal([]byte(body), &s); err != nil {
			return "", err
		}
		return s, nil
	case TypeInt:
		var n int64
		if err := json.Unmarshal([]byte(body), &n); err != nil {
			return "", err
		}
		return strconv.FormatInt(n, 10), nil
	case TypeBool:
		var b bool
		if err := json.Unmarshal([]byte(body), &b); err != nil {
			return "", err
		}
		return strconv.FormatBool(b), nil
	case TypeDuration:
		var s string
		if err := json.Unmarshal([]byte(body), &s); err != nil {
			return "", err
		}
		return s, nil
	case TypeJSON:
		return body, nil
	}
	return "", fmt.Errorf("settings: decodeRuntimeJSON: unsupported type %s", t)
}

// rawByName fetches the cached raw value or empty string.
func (s *OperationalStore) rawByName(name string) string {
	snap := s.cache.Load()
	if snap == nil {
		return ""
	}
	return snap.values[name]
}

// --- typed getters (one per operational field) ---

func (s *OperationalStore) HTTPPublicURL() string      { return s.rawByName("http.public_url") }
func (s *OperationalStore) GRPCPublicEndpoint() string { return s.rawByName("grpc.public_endpoint") }

func (s *OperationalStore) PasswordMinLength() int {
	n, _ := strconv.Atoi(s.rawByName("auth.password_min_length"))
	if n == 0 {
		return 10
	}
	return n
}

func (s *OperationalStore) RetentionJSON() string { return s.rawByName("retention") }
func (s *OperationalStore) GeoIPJSON() string     { return s.rawByName("geoip") }

func (s *OperationalStore) UpdatesChannel() string {
	v := s.rawByName("updates.channel")
	if v == "" {
		return "stable"
	}
	return v
}
func (s *OperationalStore) UpdatesAllowPrerelease() bool {
	b, _ := strconv.ParseBool(s.rawByName("updates.allow_prerelease"))
	return b
}
