// Package fleet owns the create/update/delete lifecycle for fleet
// groups and the integrations scaffolded on top of them. The service
// is the single orchestration entry point for the HTTP layer: it
// validates inputs, generates UUIDs, runs multi-table reassignment
// transactions, and stamps audit-friendly timestamps.
//
// The service does not hold any in-memory state — every operation
// reads and writes through storage.Store.
package fleet

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/lost-coder/panvex/internal/controlplane/fleet/integrations"
	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

// Validation errors returned to HTTP handlers for mapping to HTTP
// status codes. Handlers switch on errors.Is — do not compare
// messages.
var (
	ErrNameRequired          = errors.New("fleet group name is required")
	ErrNameInvalid           = errors.New("fleet group name must be a URL-safe slug (lowercase letters, digits, hyphens)")
	ErrLabelRequired         = errors.New("fleet group label is required")
	ErrLabelTooLong          = errors.New("fleet group label is too long")
	ErrDescriptionTooLong    = errors.New("fleet group description is too long")
	ErrNameTooLong           = errors.New("fleet group name is too long")
	ErrNameInUse             = errors.New("fleet group name is already in use")
	ErrReassignTargetSame    = errors.New("reassign target cannot be the group being deleted")
	ErrReassignTargetMissing = errors.New("reassign target is required: the group has members that must be moved before deletion")
)

const (
	// Matches the DB column caps: keep in sync with any future ALTER.
	maxNameLength        = 64
	maxLabelLength       = 128
	maxDescriptionLength = 512

	// Name doubles as URL segment and CLI identifier — restrict to
	// lowercase alphanumerics plus hyphens so it round-trips through
	// logs, greps, and shell pipelines without escaping.
	defaultFleetGroupName  = "default"
	defaultFleetGroupLabel = "Default"
)

var nameRegexp = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)

// Service orchestrates fleet-group lifecycle operations. Instantiate
// with NewService; the zero value is not usable.
type Service struct {
	store storage.Store
	now   func() time.Time
	// newID is the UUID factory — overridable in tests to keep
	// fixtures deterministic.
	newID                 func() string
	integrations          *integrations.IntegrationRegistry
	providerIntegrations  *integrations.ProviderRegistry
}

// NewService wires a Service against the given store + clock. When
// `now` is nil, time.Now (UTC) is used. Integration registries are
// created empty; call RegisterIntegrationKind / RegisterProviderKind
// (or pass them in via SetRegistries) at boot.
func NewService(store storage.Store, now func() time.Time) *Service {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &Service{
		store:                store,
		now:                  now,
		newID:                uuid.NewString,
		integrations:         integrations.NewIntegrationRegistry(),
		providerIntegrations: integrations.NewProviderRegistry(),
	}
}

// IntegrationRegistry returns the registry of installable integration
// kinds. Consumers register kinds (e.g. dns-rr) via the returned
// value at boot.
func (s *Service) IntegrationRegistry() *integrations.IntegrationRegistry { return s.integrations }

// ProviderRegistry returns the registry of shared-provider kinds
// (e.g. cloudflare).
func (s *Service) ProviderRegistry() *integrations.ProviderRegistry { return s.providerIntegrations }

// CreateInput is the validated shape accepted by Create. Name is the
// immutable slug. Label and Description are the editable surface.
type CreateInput struct {
	Name        string
	Label       string
	Description string
}

// UpdateInput captures the editable fields on a fleet group. Name
// cannot be changed post-create.
type UpdateInput struct {
	Label       string
	Description string
}

func (s *Service) Create(ctx context.Context, input CreateInput) (storage.FleetGroupRecord, error) {
	name := strings.TrimSpace(strings.ToLower(input.Name))
	label := strings.TrimSpace(input.Label)
	description := strings.TrimSpace(input.Description)

	if err := validateName(name); err != nil {
		return storage.FleetGroupRecord{}, err
	}
	if label == "" {
		// Fall back to name for label so operators who only care about
		// the slug don't have to type it twice.
		label = name
	}
	if err := validateLabel(label); err != nil {
		return storage.FleetGroupRecord{}, err
	}
	if err := validateDescription(description); err != nil {
		return storage.FleetGroupRecord{}, err
	}

	// Cheap pre-check for a friendly error. The final guard is the
	// DB's UNIQUE(name) constraint, which CreateFleetGroup surfaces as
	// a driver error; we translate here for the HTTP path.
	if _, err := s.store.GetFleetGroupByName(ctx, name); err == nil {
		return storage.FleetGroupRecord{}, ErrNameInUse
	} else if !errors.Is(err, storage.ErrNotFound) {
		return storage.FleetGroupRecord{}, err
	}

	now := s.now().UTC()
	record := storage.FleetGroupRecord{
		ID:          s.newID(),
		Name:        name,
		Label:       label,
		Description: description,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := s.store.CreateFleetGroup(ctx, record); err != nil {
		// Race on UNIQUE(name): surface the friendly error regardless
		// of which SQL driver wraps the constraint violation.
		if isUniqueViolation(err) {
			return storage.FleetGroupRecord{}, ErrNameInUse
		}
		return storage.FleetGroupRecord{}, err
	}
	return record, nil
}

func (s *Service) Update(ctx context.Context, id string, input UpdateInput) (storage.FleetGroupRecord, error) {
	existing, err := s.store.GetFleetGroup(ctx, id)
	if err != nil {
		return storage.FleetGroupRecord{}, err
	}

	label := strings.TrimSpace(input.Label)
	description := strings.TrimSpace(input.Description)
	if label == "" {
		label = existing.Label
	}
	if err := validateLabel(label); err != nil {
		return storage.FleetGroupRecord{}, err
	}
	if err := validateDescription(description); err != nil {
		return storage.FleetGroupRecord{}, err
	}

	existing.Label = label
	existing.Description = description
	existing.UpdatedAt = s.now().UTC()
	if err := s.store.UpdateFleetGroup(ctx, existing); err != nil {
		return storage.FleetGroupRecord{}, err
	}
	return existing, nil
}

func (s *Service) Get(ctx context.Context, id string) (storage.FleetGroupRecord, error) {
	return s.store.GetFleetGroup(ctx, id)
}

func (s *Service) GetByName(ctx context.Context, name string) (storage.FleetGroupRecord, error) {
	return s.store.GetFleetGroupByName(ctx, strings.ToLower(strings.TrimSpace(name)))
}

func (s *Service) List(ctx context.Context) ([]storage.FleetGroupRecord, error) {
	return s.store.ListFleetGroups(ctx)
}

// DeletionPreview reports how many FK rows reference the group. Used
// by the HTTP confirmation dialog so the operator sees the blast
// radius (and can pick a reassign target) before confirming.
func (s *Service) DeletionPreview(ctx context.Context, id string) (storage.ReassignCounts, error) {
	if _, err := s.store.GetFleetGroup(ctx, id); err != nil {
		return storage.ReassignCounts{}, err
	}
	return s.store.CountFleetGroupMembers(ctx, id)
}

// Delete removes a fleet group after moving every FK reference to
// `reassignTo`. When the group has no members, reassignTo may be
// empty. All three updates + the final delete run inside one
// transaction so a crash can never leave dangling FKs or an
// already-reassigned-but-not-deleted group.
func (s *Service) Delete(ctx context.Context, id string, reassignTo string) (storage.ReassignCounts, error) {
	if reassignTo != "" && reassignTo == id {
		return storage.ReassignCounts{}, ErrReassignTargetSame
	}

	var moved storage.ReassignCounts
	err := s.store.Transact(ctx, func(tx storage.Store) error {
		if _, err := tx.GetFleetGroup(ctx, id); err != nil {
			return err
		}
		counts, err := tx.CountFleetGroupMembers(ctx, id)
		if err != nil {
			return err
		}
		if hasFleetGroupMembers(counts) {
			reassigned, err := reassignFleetGroupMembers(ctx, tx, id, reassignTo)
			if err != nil {
				return err
			}
			moved = reassigned
		}
		return tx.DeleteFleetGroup(ctx, id)
	})
	return moved, err
}

func hasFleetGroupMembers(counts storage.ReassignCounts) bool {
	return counts.Agents+counts.EnrollmentTokens+counts.ClientAssignments > 0
}

// reassignFleetGroupMembers validates the reassignment target and moves
// every FK row pointing at `id` over to `reassignTo`. Pulled out of
// Delete so the transactional callback stays under the cognitive-
// complexity budget while preserving the same all-or-nothing semantics.
func reassignFleetGroupMembers(ctx context.Context, tx storage.Store, id, reassignTo string) (storage.ReassignCounts, error) {
	if reassignTo == "" {
		return storage.ReassignCounts{}, ErrReassignTargetMissing
	}
	if _, err := tx.GetFleetGroup(ctx, reassignTo); err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return storage.ReassignCounts{}, fmt.Errorf("reassign target %q not found", reassignTo)
		}
		return storage.ReassignCounts{}, err
	}
	return tx.ReassignFleetGroupMembers(ctx, id, reassignTo)
}

// EnsureDefault guarantees a "default" fleet group exists. Called at
// control-plane startup: fresh databases need at least one group for
// enrollment tokens to reference. Returns the existing row if already
// present, or a new one if freshly seeded.
func (s *Service) EnsureDefault(ctx context.Context) (storage.FleetGroupRecord, error) {
	if existing, err := s.store.GetFleetGroupByName(ctx, defaultFleetGroupName); err == nil {
		return existing, nil
	} else if !errors.Is(err, storage.ErrNotFound) {
		return storage.FleetGroupRecord{}, err
	}
	return s.Create(ctx, CreateInput{
		Name:        defaultFleetGroupName,
		Label:       defaultFleetGroupLabel,
		Description: "",
	})
}

func validateName(name string) error {
	if name == "" {
		return ErrNameRequired
	}
	if len(name) > maxNameLength {
		return ErrNameTooLong
	}
	if !nameRegexp.MatchString(name) {
		return ErrNameInvalid
	}
	return nil
}

func validateLabel(label string) error {
	if label == "" {
		return ErrLabelRequired
	}
	if len(label) > maxLabelLength {
		return ErrLabelTooLong
	}
	return nil
}

func validateDescription(description string) error {
	if len(description) > maxDescriptionLength {
		return ErrDescriptionTooLong
	}
	return nil
}

// isUniqueViolation recognises both the Postgres unique_violation
// SQLSTATE (23505) and the SQLite "UNIQUE constraint failed" textual
// error. Both paths surface via driver-specific errors that do not
// implement a common interface, so substring checks are the least
// fragile cross-backend discriminator.
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "UNIQUE constraint failed") ||
		strings.Contains(msg, "duplicate key value") ||
		strings.Contains(msg, "SQLSTATE 23505")
}

// ---- Integrations ---------------------------------------------------

// InstallIntegrationInput captures the fields needed to install an
// integration onto a fleet group.
type InstallIntegrationInput struct {
	Kind       string
	ProviderID *string
	Config     json.RawMessage
	Enabled    bool
}

// UpdateIntegrationInput describes a partial update. All fields are
// applied verbatim (no merging).
type UpdateIntegrationInput struct {
	ProviderID *string
	Config     json.RawMessage
	Enabled    bool
}

// InstallIntegration validates the config against the integration
// registry and persists a new FleetGroupIntegrationRecord. Fails with
// ErrUnknownKind / ErrProviderRequired / ErrProviderKindMismatch when
// the kind / provider combination is invalid, storage.ErrNotFound when
// the fleet group or provider is missing, and isUniqueViolation when
// the (group, kind) pair is already installed.
func (s *Service) InstallIntegration(ctx context.Context, fleetGroupID string, input InstallIntegrationInput) (storage.FleetGroupIntegrationRecord, error) {
	if _, err := s.store.GetFleetGroup(ctx, fleetGroupID); err != nil {
		return storage.FleetGroupIntegrationRecord{}, err
	}

	var providerRecord *storage.IntegrationProviderRecord
	if input.ProviderID != nil && *input.ProviderID != "" {
		p, err := s.store.GetIntegrationProvider(ctx, *input.ProviderID)
		if err != nil {
			return storage.FleetGroupIntegrationRecord{}, err
		}
		providerRecord = &p
	}

	if err := s.integrations.Validate(input.Kind, input.Config, providerRecord); err != nil {
		return storage.FleetGroupIntegrationRecord{}, err
	}

	now := s.now().UTC()
	record := storage.FleetGroupIntegrationRecord{
		ID:           s.newID(),
		FleetGroupID: fleetGroupID,
		Kind:         input.Kind,
		ProviderID:   input.ProviderID,
		Config:       []byte(input.Config),
		Enabled:      input.Enabled,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := s.store.CreateFleetGroupIntegration(ctx, record); err != nil {
		return storage.FleetGroupIntegrationRecord{}, err
	}
	return record, nil
}

// UpdateIntegration patches an existing install. The record's Kind
// is immutable; to change it, uninstall and reinstall.
func (s *Service) UpdateIntegration(ctx context.Context, integrationID string, input UpdateIntegrationInput) (storage.FleetGroupIntegrationRecord, error) {
	existing, err := s.store.GetFleetGroupIntegration(ctx, integrationID)
	if err != nil {
		return storage.FleetGroupIntegrationRecord{}, err
	}

	var providerRecord *storage.IntegrationProviderRecord
	if input.ProviderID != nil && *input.ProviderID != "" {
		p, err := s.store.GetIntegrationProvider(ctx, *input.ProviderID)
		if err != nil {
			return storage.FleetGroupIntegrationRecord{}, err
		}
		providerRecord = &p
	}

	if err := s.integrations.Validate(existing.Kind, input.Config, providerRecord); err != nil {
		return storage.FleetGroupIntegrationRecord{}, err
	}

	existing.ProviderID = input.ProviderID
	existing.Config = []byte(input.Config)
	existing.Enabled = input.Enabled
	existing.UpdatedAt = s.now().UTC()
	if err := s.store.UpdateFleetGroupIntegration(ctx, existing); err != nil {
		return storage.FleetGroupIntegrationRecord{}, err
	}
	return existing, nil
}

// UninstallIntegration removes one integration row. No cascade on
// provider — the shared provider can still back other installs.
func (s *Service) UninstallIntegration(ctx context.Context, integrationID string) error {
	return s.store.DeleteFleetGroupIntegration(ctx, integrationID)
}

// GetIntegration is a thin pass-through that preserves service as
// the HTTP handler's single dependency.
func (s *Service) GetIntegration(ctx context.Context, integrationID string) (storage.FleetGroupIntegrationRecord, error) {
	return s.store.GetFleetGroupIntegration(ctx, integrationID)
}

// ListIntegrations returns every installed integration for a group.
func (s *Service) ListIntegrations(ctx context.Context, fleetGroupID string) ([]storage.FleetGroupIntegrationRecord, error) {
	return s.store.ListFleetGroupIntegrations(ctx, fleetGroupID)
}

// ---- Providers ------------------------------------------------------

// CreateProviderInput captures the fields needed to store a shared
// credential bundle.
type CreateProviderInput struct {
	Kind   string
	Label  string
	Config json.RawMessage
}

// UpdateProviderInput describes a partial update (label + config).
// Kind is immutable once created — change it by creating a new row.
type UpdateProviderInput struct {
	Label  string
	Config json.RawMessage
}

func (s *Service) CreateProvider(ctx context.Context, input CreateProviderInput) (storage.IntegrationProviderRecord, error) {
	if err := s.providerIntegrations.Validate(input.Kind, input.Config); err != nil {
		return storage.IntegrationProviderRecord{}, err
	}
	now := s.now().UTC()
	record := storage.IntegrationProviderRecord{
		ID:        s.newID(),
		Kind:      input.Kind,
		Label:     input.Label,
		Config:    []byte(input.Config),
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := s.store.CreateIntegrationProvider(ctx, record); err != nil {
		return storage.IntegrationProviderRecord{}, err
	}
	return record, nil
}

func (s *Service) UpdateProvider(ctx context.Context, providerID string, input UpdateProviderInput) (storage.IntegrationProviderRecord, error) {
	existing, err := s.store.GetIntegrationProvider(ctx, providerID)
	if err != nil {
		return storage.IntegrationProviderRecord{}, err
	}
	if err := s.providerIntegrations.Validate(existing.Kind, input.Config); err != nil {
		return storage.IntegrationProviderRecord{}, err
	}
	existing.Label = input.Label
	existing.Config = []byte(input.Config)
	existing.UpdatedAt = s.now().UTC()
	if err := s.store.UpdateIntegrationProvider(ctx, existing); err != nil {
		return storage.IntegrationProviderRecord{}, err
	}
	return existing, nil
}

func (s *Service) GetProvider(ctx context.Context, providerID string) (storage.IntegrationProviderRecord, error) {
	return s.store.GetIntegrationProvider(ctx, providerID)
}

func (s *Service) ListProviders(ctx context.Context) ([]storage.IntegrationProviderRecord, error) {
	return s.store.ListIntegrationProviders(ctx)
}

// DeleteProvider removes the provider row. fleet_group_integrations
// rows that reference it stay (their provider_id becomes NULL via
// ON DELETE SET NULL). Callers should re-validate dependent
// integrations after a provider is removed — an integration that
// required a provider will reject its own Validate call on the next
// reconcile.
func (s *Service) DeleteProvider(ctx context.Context, providerID string) error {
	return s.store.DeleteIntegrationProvider(ctx, providerID)
}
