package updates

import (
	"context"
	"encoding/json"
	"sync"
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
// satisfies it structurally. Settings, State, SelfUpdate and PendingAgentUpdates
// are four independent keys.
type SettingsStore interface {
	GetUpdateSettings(ctx context.Context) (json.RawMessage, error)
	PutUpdateSettings(ctx context.Context, settings json.RawMessage) error
	GetUpdateState(ctx context.Context) (json.RawMessage, error)
	PutUpdateState(ctx context.Context, state json.RawMessage) error
	GetPanelSelfUpdate(ctx context.Context) (json.RawMessage, error)
	PutPanelSelfUpdate(ctx context.Context, raw json.RawMessage) error
	GetPendingAgentUpdates(ctx context.Context) (json.RawMessage, error)
	PutPendingAgentUpdates(ctx context.Context, raw json.RawMessage) error
}

// Service owns the persistence of the update Settings and State blobs. The
// self-update orchestration (download/verify/install) and the periodic check
// worker stay in the server package — they drive server runtime state
// (background wait-group, restart hook, settings mutex), not the store.
type Service struct {
	store SettingsStore
	// pendingMu serializes the read-modify-write of the pending-agent-update
	// map: operator dispatches and reconnect reconciles mutate the same blob
	// from independent goroutines.
	pendingMu sync.Mutex
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

// pendingAgentUpdates maps agent ID -> the agent version an operator asked for
// and the node has not reported yet. It is the desired state behind the
// one-shot agent.self-update job: the job itself expires after its TTL, so an
// offline node would otherwise lose the request silently.
type pendingAgentUpdates map[string]string

// loadPendingLocked reads the persisted map. An absent blob is an empty map,
// not an error. Callers must hold pendingMu.
func (s *Service) loadPendingLocked(ctx context.Context) (pendingAgentUpdates, error) {
	data, err := s.store.GetPendingAgentUpdates(ctx)
	if err != nil {
		return nil, err
	}
	pending := pendingAgentUpdates{}
	if len(data) > 0 {
		if err := json.Unmarshal(data, &pending); err != nil {
			return nil, err
		}
	}
	return pending, nil
}

// savePendingLocked persists the map. Callers must hold pendingMu.
func (s *Service) savePendingLocked(ctx context.Context, pending pendingAgentUpdates) error {
	data, err := json.Marshal(pending)
	if err != nil {
		return err
	}
	return s.store.PutPendingAgentUpdates(ctx, data)
}

// SetPendingAgentUpdate records that agentID should reach version. A later call
// for the same agent replaces the older target — the newest operator click wins.
func (s *Service) SetPendingAgentUpdate(ctx context.Context, agentID, version string) error {
	s.pendingMu.Lock()
	defer s.pendingMu.Unlock()
	pending, err := s.loadPendingLocked(ctx)
	if err != nil {
		return err
	}
	pending[agentID] = version
	return s.savePendingLocked(ctx, pending)
}

// ClearPendingAgentUpdate drops agentID's pending target. Clearing an agent
// that has none is a no-op and writes nothing: the reconcile path calls this
// on every reconnect where the reported version already matches.
func (s *Service) ClearPendingAgentUpdate(ctx context.Context, agentID string) error {
	s.pendingMu.Lock()
	defer s.pendingMu.Unlock()
	pending, err := s.loadPendingLocked(ctx)
	if err != nil {
		return err
	}
	if _, ok := pending[agentID]; !ok {
		return nil
	}
	delete(pending, agentID)
	return s.savePendingLocked(ctx, pending)
}

// PendingAgentUpdate returns the version agentID was asked to reach, and
// whether such a request is outstanding.
func (s *Service) PendingAgentUpdate(ctx context.Context, agentID string) (string, bool, error) {
	s.pendingMu.Lock()
	defer s.pendingMu.Unlock()
	pending, err := s.loadPendingLocked(ctx)
	if err != nil {
		return "", false, err
	}
	version, ok := pending[agentID]
	return version, ok, nil
}
