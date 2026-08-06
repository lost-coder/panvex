package updates

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
	"github.com/lost-coder/panvex/internal/restartspec"
)

// ErrInvalidStrategy is the sentinel wrapped by every validation failure in
// Put, so HTTP handlers can map any of them to 400 with errors.Is, without
// enumerating each concrete message.
var ErrInvalidStrategy = errors.New("updates: invalid telemt update strategy")

// Valid Mode values for a Strategy. "binary" means a supported process
// supervisor fronts telemt and an in-place binary swap + supervised restart
// is safe; "docker" means telemt runs in a container (updates go through the
// image, never a binary swap); "none" means no in-place update path exists.
const (
	StrategyModeBinary = "binary"
	StrategyModeDocker = "docker"
	StrategyModeNone   = "none"
)

// assetFlavors are the recognised non-default AssetFlavor values. "" is
// always valid (the default asset).
const assetFlavorV3 = "v3"

// Strategy is the domain form of an agent's Telemt update strategy — a
// timeless mirror of storage.AgentUpdateStrategyRecord (no CreatedAt/UpdatedAt,
// those are storage bookkeeping the HTTP layer never needs).
type Strategy struct {
	Mode        string
	RestartSpec string
	BinaryPath  string
	AssetFlavor string
}

// StrategyRepo is the subset of storage.Store StrategyService needs.
// storage.Store satisfies it structurally, so no adapter is required.
type StrategyRepo interface {
	GetAgentUpdateStrategy(ctx context.Context, agentID string) (storage.AgentUpdateStrategyRecord, error)
	UpsertAgentUpdateStrategy(ctx context.Context, rec storage.AgentUpdateStrategyRecord) error
}

// StrategyService persists and reads the per-agent Telemt update strategy.
// Configtargets-pattern thin domain service (P8.2f exemplar): the store
// round-trips and validation live here; HTTP handlers keep request
// decoding and the probe lookup (LiveStore, not persisted).
type StrategyService struct {
	repo StrategyRepo
	now  func() time.Time
}

// NewStrategyService constructs a StrategyService. now supplies the
// CreatedAt/UpdatedAt clock (injected so tests can pin it).
func NewStrategyService(repo StrategyRepo, now func() time.Time) *StrategyService {
	return &StrategyService{repo: repo, now: now}
}

// Get returns the persisted strategy for agentID. A missing record is NOT an
// error — it yields the zero Strategy and ok=false (the agent has never had
// a strategy configured). A non-NotFound store error is propagated.
func (s *StrategyService) Get(ctx context.Context, agentID string) (Strategy, bool, error) {
	rec, err := s.repo.GetAgentUpdateStrategy(ctx, agentID)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return Strategy{}, false, nil
		}
		return Strategy{}, false, err
	}
	return Strategy{
		Mode:        rec.Mode,
		RestartSpec: rec.RestartSpec,
		BinaryPath:  rec.BinaryPath,
		AssetFlavor: rec.AssetFlavor,
	}, true, nil
}

// Put validates and stores st for agentID, preserving the CreatedAt of an
// existing record (a fresh record stamps CreatedAt = now), matching the
// configtargets.Service.Upsert contract. Returns an error wrapping
// ErrInvalidStrategy when st fails validation — the store is never touched
// in that case.
func (s *StrategyService) Put(ctx context.Context, agentID string, st Strategy) error {
	if err := validateStrategy(st); err != nil {
		return err
	}

	now := s.now()
	createdAt := now
	if existing, err := s.repo.GetAgentUpdateStrategy(ctx, agentID); err == nil {
		createdAt = existing.CreatedAt
	} else if !errors.Is(err, storage.ErrNotFound) {
		return err
	}

	return s.repo.UpsertAgentUpdateStrategy(ctx, storage.AgentUpdateStrategyRecord{
		AgentID:     agentID,
		Mode:        st.Mode,
		RestartSpec: st.RestartSpec,
		BinaryPath:  st.BinaryPath,
		AssetFlavor: st.AssetFlavor,
		CreatedAt:   createdAt,
		UpdatedAt:   now,
	})
}

// validateStrategy enforces the Telemt Update v1 strategy contract: Mode
// must be one of the three known values; AssetFlavor is either empty or
// "v3"; and Mode=="binary" additionally requires a RestartSpec that parses
// via restartspec.Parse (the exact rules the agent applies) and an absolute
// BinaryPath — the two fields the panel needs to safely dispatch a binary
// swap + supervised restart.
func validateStrategy(st Strategy) error {
	switch st.Mode {
	case StrategyModeBinary, StrategyModeDocker, StrategyModeNone:
	default:
		return fmt.Errorf("%w: mode must be one of %q, %q, %q (got %q)",
			ErrInvalidStrategy, StrategyModeBinary, StrategyModeDocker, StrategyModeNone, st.Mode)
	}

	switch st.AssetFlavor {
	case "", assetFlavorV3:
	default:
		return fmt.Errorf("%w: asset_flavor must be empty or %q (got %q)", ErrInvalidStrategy, assetFlavorV3, st.AssetFlavor)
	}

	if st.Mode != StrategyModeBinary {
		return nil
	}

	if strings.TrimSpace(st.RestartSpec) == "" {
		return fmt.Errorf("%w: restart_spec is required for mode=%q", ErrInvalidStrategy, StrategyModeBinary)
	}
	if _, err := restartspec.Parse(st.RestartSpec); err != nil {
		return fmt.Errorf("%w: restart_spec: %w", ErrInvalidStrategy, err)
	}
	if strings.TrimSpace(st.BinaryPath) == "" {
		return fmt.Errorf("%w: binary_path is required for mode=%q", ErrInvalidStrategy, StrategyModeBinary)
	}
	if !filepath.IsAbs(st.BinaryPath) {
		return fmt.Errorf("%w: binary_path must be an absolute path (got %q)", ErrInvalidStrategy, st.BinaryPath)
	}
	return nil
}
