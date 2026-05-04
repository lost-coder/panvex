package geoip

// Mode selects how .mmdb files are acquired.
type Mode = string

const (
	ModeDisabled Mode = ""
	ModeAuto     Mode = "auto"
	ModeURL      Mode = "url"
	ModeLocal    Mode = "local"
)

// Kind identifies which database a Source / SourceState refers to.
type Kind string

const (
	KindCity Kind = "city"
	KindASN  Kind = "asn"
)

// Settings is the persisted operator-managed configuration.
type Settings struct {
	Mode Mode   `json:"mode"`
	City Source `json:"city"`
	ASN  Source `json:"asn"`
}

// SourceFor returns the Source for the given Kind. Unknown Kinds return
// the zero Source — callers treat that as disabled.
func (s Settings) SourceFor(k Kind) Source {
	switch k {
	case KindCity:
		return s.City
	case KindASN:
		return s.ASN
	default:
		return Source{}
	}
}

// Source is the per-database configuration carried inside Settings.
type Source struct {
	Enabled   bool   `json:"enabled"`
	URL       string `json:"url,omitempty"`
	LocalPath string `json:"local_path,omitempty"`
}

// State is the persisted runtime state — last check / update / error
// per database. Independent from Settings so the worker can write it
// without contending with operator edits.
type State struct {
	City SourceState `json:"city"`
	ASN  SourceState `json:"asn"`
}

// ForKind returns a pointer to the SourceState for the given Kind so
// callers can mutate State in place.
func (s *State) ForKind(k Kind) *SourceState {
	switch k {
	case KindCity:
		return &s.City
	case KindASN:
		return &s.ASN
	default:
		return nil
	}
}

// SourceState is the per-database runtime state.
type SourceState struct {
	LastCheckedAt int64  `json:"last_checked_at,omitempty"`
	LastUpdatedAt int64  `json:"last_updated_at,omitempty"`
	ETag          string `json:"etag,omitempty"`
	Path          string `json:"path,omitempty"`
	SizeBytes     int64  `json:"size_bytes,omitempty"`
	Error         string `json:"error,omitempty"`
}
