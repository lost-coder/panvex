package server

import (
	"errors"
	"log"
	"net/http"
	"net/url"
	"strings"

	"github.com/panvex/panvex/internal/controlplane/auth"
)

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	TotpCode string `json:"totp_code"`
}

type meResponse struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	Role        string `json:"role"`
	TotpEnabled bool   `json:"totp_enabled"`
}

type updateTotpRequest struct {
	Password string `json:"password"`
	TotpCode string `json:"totp_code"`
}

type totpSetupResponse struct {
	Secret     string `json:"secret"`
	OTPAuthURL string `json:"otpauth_url"`
}

type totpStatusResponse struct {
	TotpEnabled bool `json:"totp_enabled"`
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
				log.Printf("auth login failed: %v", err)
				writeError(w, http.StatusInternalServerError, "internal error")
			}
			return
		}

		http.SetCookie(w, &http.Cookie{
			Name:     sessionCookieName,
			Value:    session.ID,
			Path:     "/",
			HttpOnly: true,
			SameSite: http.SameSiteStrictMode,
			Secure:   s.sessionCookieSecure(r),
		})
		s.appendAuditWithContext(r.Context(), session.UserID, "auth.login", session.ID, map[string]any{
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
			Secure:   s.sessionCookieSecure(r),
		})
		s.appendAuditWithContext(r.Context(), session.UserID, "auth.logout", session.ID, nil)

		w.WriteHeader(http.StatusNoContent)
	}
}

func (s *Server) handleMe() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, user, err := s.requireSession(r)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		writeJSON(w, http.StatusOK, meResponse{
			ID:          user.ID,
			Username:    user.Username,
			Role:        string(user.Role),
			TotpEnabled: user.TotpEnabled,
		})
	}
}

func (s *Server) handleTotpSetup() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, user, err := s.requireSession(r)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		secret, err := s.auth.StartTotpSetup(user.ID, s.now())
		if err != nil {
			log.Printf("start totp setup failed for user %q: %v", user.ID, err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}

		s.appendAuditWithContext(r.Context(), session.UserID, "auth.totp.setup_started", user.ID, nil)
		writeJSON(w, http.StatusOK, totpSetupResponse{
			Secret:     secret,
			OTPAuthURL: buildTotpAuthURL(user.Username, secret),
		})
	}
}

func (s *Server) handleTotpEnable() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, user, err := s.requireSession(r)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		var request updateTotpRequest
		if err := decodeJSON(r, &request); err != nil {
			writeError(w, http.StatusBadRequest, "invalid totp payload")
			return
		}

		updatedUser, err := s.auth.EnableTotp(user.ID, request.Password, request.TotpCode, s.now())
		if err != nil {
			switch {
			case errors.Is(err, auth.ErrInvalidCredentials), errors.Is(err, auth.ErrTotpRequired), errors.Is(err, auth.ErrInvalidTotpCode):
				writeError(w, http.StatusUnauthorized, err.Error())
			case errors.Is(err, auth.ErrTotpSetupNotFound):
				writeError(w, http.StatusBadRequest, err.Error())
			default:
				log.Printf("enable totp failed for user %q: %v", user.ID, err)
				writeError(w, http.StatusInternalServerError, "internal error")
			}
			return
		}

		s.appendAuditWithContext(r.Context(), session.UserID, "auth.totp.enabled", updatedUser.ID, nil)
		writeJSON(w, http.StatusOK, totpStatusResponse{
			TotpEnabled: updatedUser.TotpEnabled,
		})
	}
}

func (s *Server) handleTotpDisable() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, user, err := s.requireSession(r)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		var request updateTotpRequest
		if err := decodeJSON(r, &request); err != nil {
			writeError(w, http.StatusBadRequest, "invalid totp payload")
			return
		}

		updatedUser, err := s.auth.DisableTotp(user.ID, request.Password, request.TotpCode, s.now())
		if err != nil {
			switch {
			case errors.Is(err, auth.ErrInvalidCredentials), errors.Is(err, auth.ErrTotpRequired), errors.Is(err, auth.ErrInvalidTotpCode):
				writeError(w, http.StatusUnauthorized, err.Error())
			default:
				log.Printf("disable totp failed for user %q: %v", user.ID, err)
				writeError(w, http.StatusInternalServerError, "internal error")
			}
			return
		}

		s.appendAuditWithContext(r.Context(), session.UserID, "auth.totp.disabled", updatedUser.ID, nil)
		writeJSON(w, http.StatusOK, totpStatusResponse{
			TotpEnabled: updatedUser.TotpEnabled,
		})
	}
}

func (s *Server) requireSession(r *http.Request) (auth.Session, auth.User, error) {
	if session, user, ok := requestAuthContext(r); ok {
		return session, user, nil
	}

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

func buildTotpAuthURL(username string, secret string) string {
	return "otpauth://totp/Panvex:" + url.PathEscape(username) + "?secret=" + url.QueryEscape(secret) + "&issuer=Panvex"
}

func (s *Server) sessionCookieSecure(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}

	forwardedProto := strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-Proto"), ",")[0])
	if strings.EqualFold(forwardedProto, "https") {
		return true
	}

	if s.panelRuntime.TLSMode == panelTLSModeDirect {
		return true
	}

	settings := s.panelSettingsSnapshot()
	if settings.HTTPPublicURL == "" {
		return false
	}

	parsedURL, err := url.Parse(settings.HTTPPublicURL)
	if err != nil {
		return false
	}

	return strings.EqualFold(parsedURL.Scheme, "https")
}
