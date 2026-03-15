package server

import (
	"errors"
	"net/http"

	"github.com/panvex/panvex/internal/controlplane/auth"
)

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	TotpCode string `json:"totp_code"`
}

type meResponse struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	Role     string `json:"role"`
}

func (s *Server) handleLogin() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var request loginRequest
		if err := decodeJSON(r, &request); err != nil {
			writeError(w, http.StatusBadRequest, "invalid login payload")
			return
		}

		session, err := s.auth.Authenticate(auth.LoginInput{
			Username: request.Username,
			Password: request.Password,
			TotpCode: request.TotpCode,
		}, s.now())
		if err != nil {
			switch {
			case errors.Is(err, auth.ErrInvalidCredentials):
				writeError(w, http.StatusUnauthorized, err.Error())
			case errors.Is(err, auth.ErrTotpRequired), errors.Is(err, auth.ErrInvalidTotpCode):
				writeError(w, http.StatusUnauthorized, err.Error())
			default:
				writeError(w, http.StatusInternalServerError, err.Error())
			}
			return
		}

		http.SetCookie(w, &http.Cookie{
			Name:     sessionCookieName,
			Value:    session.ID,
			Path:     "/",
			HttpOnly: true,
			SameSite: http.SameSiteStrictMode,
			Secure:   false,
		})
		s.appendAudit(session.UserID, "auth.login", session.ID, map[string]any{
			"username": request.Username,
		})

		writeJSON(w, http.StatusOK, map[string]string{
			"session_id": session.ID,
		})
	}
}

func (s *Server) handleLogout() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, _, err := s.requireSession(r)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		if err := s.auth.Logout(session.ID); err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		http.SetCookie(w, &http.Cookie{
			Name:     sessionCookieName,
			Value:    "",
			Path:     "/",
			HttpOnly: true,
			MaxAge:   -1,
			SameSite: http.SameSiteStrictMode,
			Secure:   false,
		})
		s.appendAudit(session.UserID, "auth.logout", session.ID, nil)

		w.WriteHeader(http.StatusNoContent)
	}
}

func (s *Server) handleMe() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, user, err := s.requireSession(r)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		s.appendAudit(session.UserID, "auth.me", session.ID, nil)
		writeJSON(w, http.StatusOK, meResponse{
			ID:       user.ID,
			Username: user.Username,
			Role:     string(user.Role),
		})
	}
}

func (s *Server) requireSession(r *http.Request) (auth.Session, auth.User, error) {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil {
		return auth.Session{}, auth.User{}, err
	}

	session, err := s.auth.GetSession(cookie.Value)
	if err != nil {
		return auth.Session{}, auth.User{}, err
	}

	user, err := s.auth.GetUserByID(session.UserID)
	if err != nil {
		return auth.Session{}, auth.User{}, err
	}

	return session, user, nil
}
