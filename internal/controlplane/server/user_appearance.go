package server

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/panvex/panvex/internal/controlplane/storage"
)

const (
	userAppearanceThemeSystem        = "system"
	userAppearanceThemeLight         = "light"
	userAppearanceThemeDark          = "dark"
	userAppearanceDensityComfortable = "comfortable"
	userAppearanceDensityCompact     = "compact"
)

var errUserAppearanceStoreRequired = errors.New("persistent store required")

// UserAppearance stores the current user's persisted appearance preferences.
type UserAppearance struct {
	Theme     string `json:"theme"`
	Density   string `json:"density"`
	UpdatedAt int64  `json:"updated_at_unix"`
}

func defaultUserAppearance() UserAppearance {
	return UserAppearance{
		Theme:     userAppearanceThemeSystem,
		Density:   userAppearanceDensityComfortable,
		UpdatedAt: 0,
	}
}

func normalizeUserAppearance(record storage.UserAppearanceRecord) UserAppearance {
	appearance := defaultUserAppearance()
	appearance.Theme = normalizeUserAppearanceTheme(record.Theme)
	appearance.Density = normalizeUserAppearanceDensity(record.Density)
	if !record.UpdatedAt.IsZero() {
		appearance.UpdatedAt = record.UpdatedAt.UTC().Unix()
	}
	return appearance
}

func normalizeUserAppearanceTheme(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case userAppearanceThemeLight:
		return userAppearanceThemeLight
	case userAppearanceThemeDark:
		return userAppearanceThemeDark
	default:
		return userAppearanceThemeSystem
	}
}

func normalizeUserAppearanceDensity(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case userAppearanceDensityCompact:
		return userAppearanceDensityCompact
	default:
		return userAppearanceDensityComfortable
	}
}

func validateUserAppearance(theme string, density string) (UserAppearance, bool) {
	normalizedTheme := strings.ToLower(strings.TrimSpace(theme))
	normalizedDensity := strings.ToLower(strings.TrimSpace(density))

	switch normalizedTheme {
	case userAppearanceThemeSystem, userAppearanceThemeLight, userAppearanceThemeDark:
	default:
		return UserAppearance{}, false
	}

	switch normalizedDensity {
	case userAppearanceDensityComfortable, userAppearanceDensityCompact:
	default:
		return UserAppearance{}, false
	}

	return UserAppearance{
		Theme:   normalizedTheme,
		Density: normalizedDensity,
	}, true
}

func userAppearanceToRecord(userID string, appearance UserAppearance) storage.UserAppearanceRecord {
	record := storage.UserAppearanceRecord{
		UserID:  userID,
		Theme:   appearance.Theme,
		Density: appearance.Density,
	}
	if appearance.UpdatedAt != 0 {
		record.UpdatedAt = time.Unix(appearance.UpdatedAt, 0).UTC()
	}
	return record
}

func (s *Server) getUserAppearance(ctx context.Context, userID string) (UserAppearance, error) {
	if s.store == nil {
		return UserAppearance{}, errUserAppearanceStoreRequired
	}

	record, err := s.store.GetUserAppearance(ctx, userID)
	if err != nil {
		return UserAppearance{}, err
	}

	return normalizeUserAppearance(record), nil
}

func (s *Server) putUserAppearance(ctx context.Context, userID string, appearance UserAppearance) error {
	if s.store == nil {
		return errUserAppearanceStoreRequired
	}

	return s.store.PutUserAppearance(ctx, userAppearanceToRecord(userID, appearance))
}
