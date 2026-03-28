package server

import (
	"errors"
	"log"
	"net/http"
)

type appearanceSettingsResponse struct {
	Theme         string `json:"theme"`
	Density       string `json:"density"`
	UpdatedAtUnix int64  `json:"updated_at_unix"`
}

type updateAppearanceSettingsRequest struct {
	Theme   string `json:"theme"`
	Density string `json:"density"`
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
			log.Printf("get user appearance failed for user %q: %v", user.ID, err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}

		writeJSON(w, http.StatusOK, appearanceSettingsResponse{
			Theme:         appearance.Theme,
			Density:       appearance.Density,
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

		appearance, ok := validateUserAppearance(request.Theme, request.Density)
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
			log.Printf("put user appearance failed for user %q: %v", user.ID, err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}

		writeJSON(w, http.StatusOK, appearanceSettingsResponse{
			Theme:         appearance.Theme,
			Density:       appearance.Density,
			UpdatedAtUnix: appearance.UpdatedAt,
		})
	}
}
