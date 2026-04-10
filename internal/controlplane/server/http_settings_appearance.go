package server

import (
	"errors"
	"net/http"
)

type appearanceSettingsResponse struct {
	Theme         string `json:"theme"`
	Density       string `json:"density"`
	HelpMode      string `json:"help_mode"`
	UpdatedAtUnix int64  `json:"updated_at_unix"`
}

type updateAppearanceSettingsRequest struct {
	Theme    string `json:"theme"`
	Density  string `json:"density"`
	HelpMode string `json:"help_mode"`
}

func (s *Server) handleGetUserAppearance() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, user, err := s.requireSession(r)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		appearance, err := s.getUserAppearance(r.Context(), user.ID)
		if err != nil {
			if errors.Is(err, errUserAppearanceStoreRequired) {
				writeError(w, http.StatusServiceUnavailable, err.Error())
				return
			}
			s.logger.Error("get user appearance failed", "user_id", user.ID, "error", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}

		writeJSON(w, http.StatusOK, appearanceSettingsResponse{
			Theme:         appearance.Theme,
			Density:       appearance.Density,
			HelpMode:      appearance.HelpMode,
			UpdatedAtUnix: appearance.UpdatedAt,
		})
	}
}

func (s *Server) handlePutUserAppearance() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, user, err := s.requireSession(r)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		var request updateAppearanceSettingsRequest
		if err := decodeJSON(r, &request); err != nil {
			writeError(w, http.StatusBadRequest, "invalid appearance payload")
			return
		}

		appearance, ok := validateUserAppearance(request.Theme, request.Density, request.HelpMode)
		if !ok {
			writeError(w, http.StatusBadRequest, "appearance values are invalid")
			return
		}
		appearance.UpdatedAt = s.now().UTC().Unix()

		if err := s.putUserAppearance(r.Context(), user.ID, appearance); err != nil {
			if errors.Is(err, errUserAppearanceStoreRequired) {
				writeError(w, http.StatusServiceUnavailable, err.Error())
				return
			}
			s.logger.Error("put user appearance failed", "user_id", user.ID, "error", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}

		writeJSON(w, http.StatusOK, appearanceSettingsResponse{
			Theme:         appearance.Theme,
			Density:       appearance.Density,
			HelpMode:      appearance.HelpMode,
			UpdatedAtUnix: appearance.UpdatedAt,
		})
	}
}
