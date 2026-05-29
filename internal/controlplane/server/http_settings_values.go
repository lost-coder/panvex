package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strings"

	"github.com/lost-coder/panvex/internal/controlplane/auth"
	"github.com/lost-coder/panvex/internal/controlplane/settings"
)

type valuesEntry struct {
	Value           any    `json:"value"`
	Source          string `json:"source"`
	SourcePath      string `json:"source_path,omitempty"`
	EnvVar          string `json:"env_var,omitempty"`
	OverriddenByEnv bool   `json:"overridden_by_env,omitempty"`
	Apply           string `json:"apply,omitempty"`
	Secret          bool   `json:"secret,omitempty"`
	Locked          bool   `json:"locked"`
	PendingRestart  bool   `json:"pending_restart,omitempty"`
	PendingValue    any    `json:"pending_value,omitempty"`
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
		Apply:      string(f.Apply),
		Secret:     f.Secret,
		Locked:     true, // bootstrap is always locked from the panel UI
	}
}

func (s *Server) operationalEntry(f settings.FieldMeta) valuesEntry {
	if s.settings == nil {
		return valuesEntry{Source: "default", Locked: true, Apply: string(f.Apply)}
	}
	raw := s.settings.RawByName(f.Name)
	overridden := s.settings.OverriddenByEnv(f.Name)
	entry := valuesEntry{
		Value:           rawToTyped(f, raw),
		Source:          string(s.settings.Source(f.Name)),
		OverriddenByEnv: overridden,
		// An env-pinned value cannot be changed from the panel until the
		// env var is unset, so present it as locked.
		Locked: overridden,
		Apply:  string(f.Apply),
	}
	// pending_restart bookkeeping
	if f.Apply == settings.ApplyRestart && s.settingsActive != nil {
		if active, ok := s.settingsActive.Get(f.Name); ok && active != raw {
			entry.PendingRestart = true
			entry.PendingValue = rawToTyped(f, raw)
			entry.Value = rawToTyped(f, active)
		}
	}
	return entry
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

func (s *Server) handleSettingsValuesPUT(w http.ResponseWriter, r *http.Request) {
	session, user, err := s.requireSession(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	if user.Role != auth.RoleAdmin {
		writeError(w, http.StatusForbidden, msgAdminRoleRequired)
		return
	}
	_ = session // retained for future audit wiring

	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json body", http.StatusBadRequest)
		return
	}
	updates := make(map[string]string, len(body))
	for k, v := range body {
		updates[k] = scalarToString(v)
	}
	who := user.Username
	if err := s.settings.Put(r.Context(), updates, who); err != nil {
		switch {
		case strings.Contains(err.Error(), "bootstrap setting"):
			http.Error(w, err.Error(), http.StatusConflict)
		default:
			http.Error(w, err.Error(), http.StatusBadRequest)
		}
		return
	}
	w.WriteHeader(http.StatusOK)
}

func scalarToString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case bool:
		if t {
			return "true"
		}
		return "false"
	case float64:
		if t == float64(int64(t)) {
			return fmt.Sprintf("%d", int64(t))
		}
		return fmt.Sprintf("%v", t)
	case nil:
		return ""
	default:
		body, err := json.Marshal(v)
		if err == nil {
			return string(body)
		}
		return fmt.Sprintf("%v", v)
	}
}
