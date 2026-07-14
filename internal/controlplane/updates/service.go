package updates

import (
	"context"
	"encoding/json"
)

// Settings controls how the panel checks for and applies updates. Moved out of
// the server package by P8.2i (was server.UpdateSettings); the json tags are
// the persisted representation and must not change.
type Settings struct {
	CheckIntervalHours  int    `json:"check_interval_hours"`
	AutoUpdatePanel     bool   `json:"auto_update_panel"`
	AutoUpdateAgents    bool   `json:"auto_update_agents"`
	GitHubRepo          string `json:"github_repo"`
	GitHubToken         string `json:"github_token,omitempty"`
	AgentDownloadSource string `json:"agent_download_source"`
}

// DefaultSettings is the check-every-6h, manual-apply baseline used when no
// settings blob has been persisted yet.
func DefaultSettings() Settings {
	return Settings{
		CheckIntervalHours:  6,
		AutoUpdatePanel:     false,
		AutoUpdateAgents:    false,
		GitHubRepo:          "lost-coder/panvex",
		AgentDownloadSource: "github",
	}
}

// State caches the latest known versions from GitHub (was server.UpdateState).
type State struct {
	LatestPanelVersion string `json:"latest_panel_version"`
	LatestAgentVersion string `json:"latest_agent_version"`
	PanelDownloadURL   string `json:"panel_download_url"`
	PanelChecksumURL   string `json:"panel_checksum_url"`
	PanelChangelog     string `json:"panel_changelog"`
	AgentChangelog     string `json:"agent_changelog"`
	LastCheckedAt      int64  `json:"last_checked_at"`
	// LastCheckError holds the operator-readable reason the most recent update
	// check failed (e.g. a GitHub rate-limit message). Empty after a
	// successful check. Surfaced in the dashboard so a failed check is visible,
	// not silent.
	LastCheckError string `json:"last_check_error,omitempty"`
}

// SelfUpdatePhase enumerates the lifecycle of one panel self-update run.
type SelfUpdatePhase string

const (
	SelfUpdateIdle           SelfUpdatePhase = "" // нет активного/показуемого обновления
	SelfUpdateDownloading    SelfUpdatePhase = "downloading"
	SelfUpdateInstalling     SelfUpdatePhase = "installing"
	SelfUpdateRestartPending SelfUpdatePhase = "restart_pending" // бинарь подменён, ждём рестарта
	SelfUpdateCompleted      SelfUpdatePhase = "completed"       // терминальная
	SelfUpdateFailed         SelfUpdatePhase = "failed"          // терминальная; Message — причина
)

// Terminal reports whether the phase is a final state for the current
// self-update run (no further transitions expected without a new run).
func (p SelfUpdatePhase) Terminal() bool {
	return p == SelfUpdateCompleted || p == SelfUpdateFailed
}

// SelfUpdateState is the persisted record of the current/last self-update.
// Stored under its own settings key so a crash at any point leaves an
// accurate phase for the next boot to finalise.
type SelfUpdateState struct {
	Phase       SelfUpdatePhase `json:"phase"`
	FromVersion string          `json:"from_version,omitempty"`
	ToVersion   string          `json:"to_version,omitempty"`
	Message     string          `json:"message,omitempty"`
	UpdatedAt   int64           `json:"updated_at,omitempty"` // unix seconds
}

// SettingsStore is the subset of storage.Store the service needs. storage.Store
// satisfies it structurally. Settings, State, and SelfUpdate are three
// independent keys.
type SettingsStore interface {
	GetUpdateSettings(ctx context.Context) (json.RawMessage, error)
	PutUpdateSettings(ctx context.Context, settings json.RawMessage) error
	GetUpdateState(ctx context.Context) (json.RawMessage, error)
	PutUpdateState(ctx context.Context, state json.RawMessage) error
	GetPanelSelfUpdate(ctx context.Context) (json.RawMessage, error)
	PutPanelSelfUpdate(ctx context.Context, raw json.RawMessage) error
}

// Service owns the persistence of the update Settings and State blobs. The
// self-update orchestration (download/verify/install) and the periodic check
// worker stay in the server package — they drive server runtime state
// (background wait-group, restart hook, settings mutex), not the store.
type Service struct {
	store SettingsStore
}

// NewService constructs a Service over a persistent store.
func NewService(store SettingsStore) *Service {
	return &Service{store: store}
}

// LoadSettings returns the persisted settings, starting from DefaultSettings so
// an absent blob yields the defaults (matching the pre-extraction restore).
func (s *Service) LoadSettings(ctx context.Context) (Settings, error) {
	settings := DefaultSettings()
	data, err := s.store.GetUpdateSettings(ctx)
	if err != nil {
		return Settings{}, err
	}
	if data != nil {
		if err := json.Unmarshal(data, &settings); err != nil {
			return Settings{}, err
		}
	}
	return settings, nil
}

// LoadState returns the persisted state (zero value when no blob exists).
func (s *Service) LoadState(ctx context.Context) (State, error) {
	var state State
	data, err := s.store.GetUpdateState(ctx)
	if err != nil {
		return State{}, err
	}
	if data != nil {
		if err := json.Unmarshal(data, &state); err != nil {
			return State{}, err
		}
	}
	return state, nil
}

// SaveSettings serializes and persists the settings blob.
func (s *Service) SaveSettings(ctx context.Context, settings Settings) error {
	data, err := json.Marshal(settings)
	if err != nil {
		return err
	}
	return s.store.PutUpdateSettings(ctx, data)
}

// SaveState serializes and persists the state blob.
func (s *Service) SaveState(ctx context.Context, state State) error {
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	return s.store.PutUpdateState(ctx, data)
}

// LoadSelfUpdate returns the persisted self-update state (zero value, i.e.
// SelfUpdateIdle, when no blob exists).
func (s *Service) LoadSelfUpdate(ctx context.Context) (SelfUpdateState, error) {
	var st SelfUpdateState
	data, err := s.store.GetPanelSelfUpdate(ctx)
	if err != nil {
		return SelfUpdateState{}, err
	}
	if data != nil {
		if err := json.Unmarshal(data, &st); err != nil {
			return SelfUpdateState{}, err
		}
	}
	return st, nil
}

// SaveSelfUpdate serializes and persists the self-update state blob.
func (s *Service) SaveSelfUpdate(ctx context.Context, st SelfUpdateState) error {
	data, err := json.Marshal(st)
	if err != nil {
		return err
	}
	return s.store.PutPanelSelfUpdate(ctx, data)
}
