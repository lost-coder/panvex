// Package settings implements the central settings registry, bootstrap
// loader, and operational store described in
// docs/superpowers/specs/2026-05-07-settings-foundation-design.md.
package settings

// Class identifies which lifecycle a setting belongs to.
type Class string

const (
	ClassBootstrap   Class = "bootstrap"
	ClassOperational Class = "operational"
)

// Source identifies where the active value came from at read time.
type Source string

const (
	SourceEnv        Source = "env"
	SourceConfigFile Source = "config_file"
	SourceDB         Source = "db"
	SourceDefault    Source = "default"
)

// ApplyTier declares how a setting change takes effect.
type ApplyTier string

const (
	// ApplyLive takes effect immediately when saved.
	ApplyLive ApplyTier = "live"
	// ApplyRestart is persisted immediately but applied only after a panel restart.
	ApplyRestart ApplyTier = "restart"
	// ApplyConfig is managed via env / config.toml / the settings CLI only;
	// never written through the panel API (e.g. storage DSN, encryption key).
	ApplyConfig ApplyTier = "config"
)

// Valid reports whether t is one of the three known tiers.
func (t ApplyTier) Valid() bool {
	switch t {
	case ApplyLive, ApplyRestart, ApplyConfig:
		return true
	default:
		return false
	}
}

// Type names the value type carried by a registered field.
type Type string

const (
	TypeInt      Type = "int"
	TypeDuration Type = "duration"
	TypeString   Type = "string"
	TypeBool     Type = "bool"
	TypeHostPort Type = "hostport"
	TypeURL      Type = "url"
	TypeEnum     Type = "enum"
	TypeJSON     Type = "json"
)

// FieldMeta is the parsed form of a `setting:"…"` tag plus the Go field name.
type FieldMeta struct {
	Name       string   // dotted setting name, e.g. "http.listen_address"
	GoField    string   // Go struct field name, e.g. "HTTPListenAddress"
	Class      Class
	Type       Type
	Default    string   // raw textual default ("" if unset)
	HasDefault bool

	Min, Max string   // range bounds; raw text
	Values   []string // allowed enum values

	// Bootstrap-only
	Env    string
	Toml   string
	Secret bool

	// Operational-only
	Store   string // "panel_settings.column" | "runtime_settings"
	Restart bool

	Desc string
}
