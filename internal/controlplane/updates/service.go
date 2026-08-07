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

	// TelemtLatestVersion is the newest stable Telemt release tag known to the
	// panel (bare semver, e.g. "3.4.25"). Checked against the fixed upstream
	// telemt/telemt repo, independently of GitHubRepo above.
	TelemtLatestVersion string `json:"telemt_latest_version"`
	// TelemtReleaseBaseURL is the release's asset base URL
	// (https://github.com/telemt/telemt/releases/download/<tag>); the agent
	// appends its own per-platform asset name when it self-updates.
	TelemtReleaseBaseURL string `json:"telemt_release_base_url"`
	// TelemtLastCheckError mirrors LastCheckError but for the Telemt release
	// check, which runs as its own try in the same tick: a failure here (or in
	// LastCheckError) never affects the other's fields or clears its error.
	TelemtLastCheckError string `json:"telemt_last_check_error"`
}

// SelfUpdatePhase enumerates the lifecycle of one panel self-update run.
type SelfUpdatePhase string

const (
	SelfUpdateIdle           SelfUpdatePhase = "" // no active/showable update
	SelfUpdateDownloading    SelfUpdatePhase = "downloading"
	SelfUpdateInstalling     SelfUpdatePhase = "installing"
	SelfUpdateRestartPending SelfUpdatePhase = "restart_pending" // binary swapped, waiting for restart
	SelfUpdateCompleted      SelfUpdatePhase = "completed"       // terminal
	SelfUpdateFailed         SelfUpdatePhase = "failed"          // terminal; Message is the reason
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
// satisfies it structurally. Settings, State, SelfUpdate, PendingAgentUpdates
// and PendingTelemtUpdates are five independent keys.
type SettingsStore interface {
	GetUpdateSettings(ctx context.Context) (json.RawMessage, error)
	PutUpdateSettings(ctx context.Context, settings json.RawMessage) error
	GetUpdateState(ctx context.Context) (json.RawMessage, error)
	PutUpdateState(ctx context.Context, state json.RawMessage) error
	GetPanelSelfUpdate(ctx context.Context) (json.RawMessage, error)
	PutPanelSelfUpdate(ctx context.Context, raw json.RawMessage) error
	GetPendingAgentUpdates(ctx context.Context) (json.RawMessage, error)
	PutPendingAgentUpdates(ctx context.Context, raw json.RawMessage) error
	// GetPendingTelemtUpdates and PutPendingTelemtUpdates mirror
	// GetPendingAgentUpdates/PutPendingAgentUpdates for telemt.update jobs
	// (Task 11): an independent agent-ID -> requested-Telemt-version map
	// under its own update_config key, so the two desired-state blobs
	// (agent binary vs. managed Telemt binary) never collide or share a
	// failure budget.
	GetPendingTelemtUpdates(ctx context.Context) (json.RawMessage, error)
	PutPendingTelemtUpdates(ctx context.Context, raw json.RawMessage) error
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

// MaxPendingAgentUpdateFailures bounds how many times a pending target may be
// delivered and REPORTED FAILED before the panel gives up and drops it. Also
// governs telemt.update's pending failure budget (Task 11) — the same
// "five genuine failures is well past a transient blip" reasoning applies to
// both, and a second constant would only invite the two to drift apart for
// no reason.
//
// Without a bound, a target the node can never reach (an agent built without
// version ldflags, a 404 release asset, a checksum mismatch) is re-enqueued on
// every reconnect forever, and the operator has no way to cancel it. Five
// consecutive failures is well past a transient download blip.
//
// Only genuine failure REPORTS count. A job that merely expires unseen because
// the node is offline is exactly the case reconcile-on-reconnect exists for,
// and must never consume the budget.
const MaxPendingAgentUpdateFailures = 5

// pendingUpdateEntry is one agent's desired update: the version an operator
// asked for and the node has not reported yet, plus how many delivered
// attempts have come back failed. Shared shape for both pending-update kinds
// below (Task 11) — they differ only in which update_config key backs them.
type pendingUpdateEntry struct {
	Version string `json:"version"`
	// Failures counts consecutive failed deliveries of THIS version. Reset by
	// a new operator click (a new decision earns a fresh budget).
	Failures int `json:"failures,omitempty"`
}

// pendingUpdateMap maps agent ID -> its desired update. It is the desired
// state behind a one-shot job (agent.self-update or telemt.update): the job
// itself expires after its TTL, so an offline node would otherwise lose the
// request silently.
type pendingUpdateMap map[string]pendingUpdateEntry

// pendingUpdateAccessor binds the generic load/save/mutate logic below to one
// of the two independent update_config keys — agent self-update
// (agentPendingAccessor) or telemt.update (telemtPendingAccessor, Task 11) —
// so the mutation logic itself is written once instead of twice.
type pendingUpdateAccessor struct {
	get func(ctx context.Context) (json.RawMessage, error)
	put func(ctx context.Context, raw json.RawMessage) error
}

func (s *Service) agentPendingAccessor() pendingUpdateAccessor {
	return pendingUpdateAccessor{get: s.store.GetPendingAgentUpdates, put: s.store.PutPendingAgentUpdates}
}

func (s *Service) telemtPendingAccessor() pendingUpdateAccessor {
	return pendingUpdateAccessor{get: s.store.GetPendingTelemtUpdates, put: s.store.PutPendingTelemtUpdates}
}

// loadPendingLocked reads the persisted map behind acc. An absent blob is an
// empty map, not an error. Callers must hold pendingMu.
func (s *Service) loadPendingLocked(ctx context.Context, acc pendingUpdateAccessor) (pendingUpdateMap, error) {
	data, err := acc.get(ctx)
	if err != nil {
		return nil, err
	}
	pending := pendingUpdateMap{}
	if len(data) > 0 {
		if err := json.Unmarshal(data, &pending); err != nil {
			return nil, err
		}
	}
	return pending, nil
}

// savePendingLocked persists the map behind acc. Callers must hold pendingMu.
func (s *Service) savePendingLocked(ctx context.Context, acc pendingUpdateAccessor, pending pendingUpdateMap) error {
	data, err := json.Marshal(pending)
	if err != nil {
		return err
	}
	return acc.put(ctx, data)
}

// setPendingUpdate records that agentID should reach version in the map
// behind acc. A later call for the same agent replaces the older target —
// the newest operator click wins and restarts the failure budget, because a
// fresh click is a fresh decision.
func (s *Service) setPendingUpdate(ctx context.Context, acc pendingUpdateAccessor, agentID, version string) error {
	s.pendingMu.Lock()
	defer s.pendingMu.Unlock()
	pending, err := s.loadPendingLocked(ctx, acc)
	if err != nil {
		return err
	}
	pending[agentID] = pendingUpdateEntry{Version: version}
	return s.savePendingLocked(ctx, acc, pending)
}

// recordPendingUpdateFailure counts one FAILED delivery of the pending target
// in the map behind acc and reports the running total plus whether the panel
// has now given up (target dropped). A failure for a version that is no
// longer pending is stale — the operator re-clicked while the old job was in
// flight — and is ignored, as is a failure for an agent with nothing pending.
func (s *Service) recordPendingUpdateFailure(ctx context.Context, acc pendingUpdateAccessor, agentID, version string) (failures int, gaveUp bool, err error) {
	s.pendingMu.Lock()
	defer s.pendingMu.Unlock()
	pending, err := s.loadPendingLocked(ctx, acc)
	if err != nil {
		return 0, false, err
	}
	entry, ok := pending[agentID]
	if !ok || entry.Version != version {
		return 0, false, nil
	}

	entry.Failures++
	if entry.Failures >= MaxPendingAgentUpdateFailures {
		delete(pending, agentID)
		if err := s.savePendingLocked(ctx, acc, pending); err != nil {
			return entry.Failures, false, err
		}
		return entry.Failures, true, nil
	}
	pending[agentID] = entry
	if err := s.savePendingLocked(ctx, acc, pending); err != nil {
		return entry.Failures, false, err
	}
	return entry.Failures, false, nil
}

// clearPendingUpdate drops agentID's pending target in the map behind acc.
// Clearing an agent that has none is a no-op and writes nothing: the
// reconcile path calls this on every reconnect where the reported version
// already matches.
func (s *Service) clearPendingUpdate(ctx context.Context, acc pendingUpdateAccessor, agentID string) error {
	s.pendingMu.Lock()
	defer s.pendingMu.Unlock()
	pending, err := s.loadPendingLocked(ctx, acc)
	if err != nil {
		return err
	}
	if _, ok := pending[agentID]; !ok {
		return nil
	}
	delete(pending, agentID)
	return s.savePendingLocked(ctx, acc, pending)
}

// clearPendingUpdateIfVersion drops agentID's pending target in the map
// behind acc only if it is still set to version. A stale/superseded caller —
// a result for a version that is no longer the current pending target
// because a later click already replaced it — is a no-op, mirroring
// recordPendingUpdateFailure's same version check. Unlike
// clearPendingUpdate this must never clear a newer target out from under a
// later operator click.
func (s *Service) clearPendingUpdateIfVersion(ctx context.Context, acc pendingUpdateAccessor, agentID, version string) error {
	s.pendingMu.Lock()
	defer s.pendingMu.Unlock()
	pending, err := s.loadPendingLocked(ctx, acc)
	if err != nil {
		return err
	}
	entry, ok := pending[agentID]
	if !ok || entry.Version != version {
		return nil
	}
	delete(pending, agentID)
	return s.savePendingLocked(ctx, acc, pending)
}

// pendingUpdateVersion returns the version agentID was asked to reach in the
// map behind acc, and whether such a request is outstanding.
func (s *Service) pendingUpdateVersion(ctx context.Context, acc pendingUpdateAccessor, agentID string) (string, bool, error) {
	s.pendingMu.Lock()
	defer s.pendingMu.Unlock()
	pending, err := s.loadPendingLocked(ctx, acc)
	if err != nil {
		return "", false, err
	}
	entry, ok := pending[agentID]
	return entry.Version, ok, nil
}

// SetPendingAgentUpdate records that agentID should reach version via a
// pending agent.self-update. See setPendingUpdate for the overwrite/budget
// contract.
func (s *Service) SetPendingAgentUpdate(ctx context.Context, agentID, version string) error {
	return s.setPendingUpdate(ctx, s.agentPendingAccessor(), agentID, version)
}

// RecordPendingAgentUpdateFailure counts one FAILED agent.self-update
// delivery. See recordPendingUpdateFailure for the stale/no-op rules.
func (s *Service) RecordPendingAgentUpdateFailure(ctx context.Context, agentID, version string) (failures int, gaveUp bool, err error) {
	return s.recordPendingUpdateFailure(ctx, s.agentPendingAccessor(), agentID, version)
}

// ClearPendingAgentUpdate drops agentID's pending agent.self-update target.
func (s *Service) ClearPendingAgentUpdate(ctx context.Context, agentID string) error {
	return s.clearPendingUpdate(ctx, s.agentPendingAccessor(), agentID)
}

// PendingAgentUpdate returns the version agentID was asked to reach via
// agent.self-update, and whether such a request is outstanding.
func (s *Service) PendingAgentUpdate(ctx context.Context, agentID string) (string, bool, error) {
	return s.pendingUpdateVersion(ctx, s.agentPendingAccessor(), agentID)
}

// SetPendingTelemtUpdate records that agentID should reach version via a
// pending telemt.update (Task 11). See setPendingUpdate for the
// overwrite/budget contract.
func (s *Service) SetPendingTelemtUpdate(ctx context.Context, agentID, version string) error {
	return s.setPendingUpdate(ctx, s.telemtPendingAccessor(), agentID, version)
}

// RecordPendingTelemtUpdateFailure counts one FAILED telemt.update delivery.
// See recordPendingUpdateFailure for the stale/no-op rules.
func (s *Service) RecordPendingTelemtUpdateFailure(ctx context.Context, agentID, version string) (failures int, gaveUp bool, err error) {
	return s.recordPendingUpdateFailure(ctx, s.telemtPendingAccessor(), agentID, version)
}

// ClearPendingTelemtUpdate drops agentID's pending telemt.update target.
func (s *Service) ClearPendingTelemtUpdate(ctx context.Context, agentID string) error {
	return s.clearPendingUpdate(ctx, s.telemtPendingAccessor(), agentID)
}

// ClearPendingTelemtUpdateIfVersion drops agentID's pending telemt.update
// target only if it is still set to version. A telemt.update job's success
// result must use this instead of ClearPendingTelemtUpdate: without the
// version check, a success report for a stale/superseded job (version A)
// would clear a newer pending target (version B) that a later operator
// click set while the stale job was still in flight, silently dropping the
// operator's most recent request.
func (s *Service) ClearPendingTelemtUpdateIfVersion(ctx context.Context, agentID, version string) error {
	return s.clearPendingUpdateIfVersion(ctx, s.telemtPendingAccessor(), agentID, version)
}

// PendingTelemtUpdate returns the version agentID was asked to reach via
// telemt.update, and whether such a request is outstanding.
func (s *Service) PendingTelemtUpdate(ctx context.Context, agentID string) (string, bool, error) {
	return s.pendingUpdateVersion(ctx, s.telemtPendingAccessor(), agentID)
}
