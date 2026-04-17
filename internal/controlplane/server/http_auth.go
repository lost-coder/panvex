package server

import (
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/lost-coder/panvex/internal/controlplane/auth"
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

		if len(request.Password) > 1024 {
			writeError(w, http.StatusBadRequest, "password exceeds maximum length")
			return
		}

		if s.loginLockout.IsLocked(request.Username, s.now()) {
			s.logger.Info("login attempt on locked account", "username", request.Username)
			writeError(w, http.StatusUnauthorized, "account temporarily locked, try again later")
			return
		}

		// P2-SEC-01: capture any pre-authentication session cookie the browser
		// carried into the login request so we can invalidate it on success.
		// This closes the session-fixation window: a cookie planted before
		// login (e.g. via XSS or a shared device) must not remain valid after
		// the victim authenticates.
		priorSessionID := ""
		if existing, err := r.Cookie(sessionCookieName); err == nil {
			priorSessionID = existing.Value
		}

		session, err := s.auth.Authenticate(auth.LoginInput{
			Username:       request.Username,
			Password:       request.Password,
			TotpCode:       request.TotpCode,
			PriorSessionID: priorSessionID,
		}, s.now())
		if err != nil {
			// Record lockout-eligible failures: wrong password or wrong TOTP code.
			if errors.Is(err, auth.ErrInvalidCredentials) || errors.Is(err, auth.ErrInvalidTotpCode) {
				if s.loginLockout.CheckAndRecordFailure(request.Username, s.now()) {
					s.logger.Info("account locked out", "username", request.Username)
					writeError(w, http.StatusUnauthorized, "account temporarily locked, try again later")
					return
				}
			}
			switch {
			case errors.Is(err, auth.ErrInvalidCredentials):
				writeErrorWithCode(w, http.StatusUnauthorized, err.Error(), "invalid_credentials")
			case errors.Is(err, auth.ErrTotpRequired):
				writeErrorWithCode(w, http.StatusUnauthorized, err.Error(), "totp_required")
			case errors.Is(err, auth.ErrInvalidTotpCode):
				writeErrorWithCode(w, http.StatusUnauthorized, err.Error(), "totp_invalid")
			case errors.Is(err, auth.ErrSessionStoreUnavailable):
				// P2-SEC-07: session persistence failed; no session was
				// created. Tell the client to retry rather than masking
				// the failure behind an in-memory-only session that would
				// silently disappear on the next control-plane restart.
				s.logger.Error("session store unavailable during login", "username", request.Username, "error", err)
				writeErrorWithCode(w, http.StatusServiceUnavailable, "session store unavailable", "session_store_unavailable")
			default:
				s.logger.Error("auth login failed", "error", err)
				writeError(w, http.StatusInternalServerError, "internal error")
			}
			return
		}

		s.loginLockout.RecordSuccess(request.Username)
		s.logger.Info("user logged in", "username", request.Username, "session_id", session.ID)

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
			"status": "ok",
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

		s.logger.Info("user logged out", "user_id", session.UserID, "session_id", session.ID)
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
			s.logger.Error("start totp setup failed", "user_id", user.ID, "error", err)
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
				s.logger.Error("enable totp failed", "user_id", user.ID, "error", err)
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
				s.logger.Error("disable totp failed", "user_id", user.ID, "error", err)
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

// trustedForwardedProto returns the first value of the X-Forwarded-Proto
// header, but only when the TCP peer is a configured trusted proxy (or
// loopback). Untrusted peers cannot influence deployment-topology decisions
// (public URL scheme, Secure cookie flag, ...) by spoofing this header.
// See DF-2 / P2-SEC-04.
func (s *Server) trustedForwardedProto(r *http.Request) string {
	if !remoteAddrIsTrustedProxy(r, s.trustedProxyCIDRs) {
		return ""
	}
	return r.Header.Get("X-Forwarded-Proto")
}

func (s *Server) sessionCookieSecure(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}

	// DF-2 / P2-SEC-04: only trust X-Forwarded-Proto when the TCP peer is a
	// configured trusted proxy (or loopback). Otherwise an attacker speaking
	// plain HTTP directly to the control-plane could send
	// `X-Forwarded-Proto: https` and trick us into marking the session cookie
	// Secure, which breaks the Secure-flag contract and can leak the cookie
	// to passive network observers on the plain-HTTP link.
	if remoteAddrIsTrustedProxy(r, s.trustedProxyCIDRs) {
		forwardedProto := strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-Proto"), ",")[0])
		if strings.EqualFold(forwardedProto, "https") {
			return true
		}
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
