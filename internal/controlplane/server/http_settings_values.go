package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"

	"github.com/lost-coder/panvex/internal/controlplane/settings"
)

type valuesEntry struct {
	Value          any    `json:"value"`
	Source         string `json:"source"`
	SourcePath     string `json:"source_path,omitempty"`
	EnvVar         string `json:"env_var,omitempty"`
	Secret         bool   `json:"secret,omitempty"`
	Locked         bool   `json:"locked"`
	PendingRestart bool   `json:"pending_restart,omitempty"`
	PendingValue   any    `json:"pending_value,omitempty"`
}

type valuesResponse struct {
	Bootstrap   map[string]valuesEntry `json:"bootstrap"`
	Operational map[string]valuesEntry `json:"operational"`
}

func (s *Server) handleSettingsValuesGET(w http.ResponseWriter, r *http.Request) {
	resp := valuesResponse{
		Bootstrap:   map[string]valuesEntry{},
		Operational: map[string]valuesEntry{},
	}
	for _, f := range settings.AllFields() {
		switch f.Class {
		case settings.ClassBootstrap:
			resp.Bootstrap[f.Name] = s.bootstrapEntry(f)
		case settings.ClassOperational:
			resp.Operational[f.Name] = s.operationalEntry(f)
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (s *Server) bootstrapEntry(f settings.FieldMeta) valuesEntry {
	var raw any = ""
	if s.bootstrap != nil {
		v := reflect.ValueOf(s.bootstrap).Elem().FieldByName(f.GoField)
		if v.IsValid() {
			raw = v.Interface()
		}
	}
	if f.Secret {
		// Redact non-empty values; empty stays empty.
		if rawStr, ok := raw.(string); ok && rawStr != "" {
			raw = "***"
		}
	}
	src := "default"
	srcPath := ""
	envVar := ""
	if info, ok := s.bootstrapSources[f.Name]; ok {
		src = string(info.Source)
		srcPath = info.SourcePath
		envVar = info.EnvVar
	}
	return valuesEntry{
		Value:      raw,
		Source:     src,
		SourcePath: srcPath,
		EnvVar:     envVar,
		Secret:     f.Secret,
		Locked:     true, // bootstrap is always locked from the panel UI
	}
}

func (s *Server) operationalEntry(f settings.FieldMeta) valuesEntry {
	if s.settings == nil {
		return valuesEntry{Source: "default", Locked: true}
	}
	raw := s.settings.RawByName(f.Name)
	return valuesEntry{
		Value:  rawToTyped(f, raw),
		Source: "db",
		Locked: false,
	}
}

func rawToTyped(f settings.FieldMeta, raw string) any {
	switch f.Type {
	case settings.TypeInt:
		var n int
		_, _ = fmt.Sscanf(raw, "%d", &n)
		return n
	case settings.TypeBool:
		return raw == "true" || raw == "1" || raw == "yes"
	case settings.TypeJSON:
		var v any
		if json.Unmarshal([]byte(raw), &v) == nil {
			return v
		}
		return raw
	}
	return raw
}
