package server

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/auth"
)

// loginAuditPersistTimeout bounds the synchronous audit-persist on login
// (B1). Long enough to survive a normal fsync / PG round-trip but short
// enough that a wedged database doesn't leave a browser hanging on the
// login screen. A failure inside this window returns 503 and the user
// retries; no session cookie is issued when the persist is not confirmed.
const loginAuditPersistTimeout = 2 * time.Second

// loginTimingFloor is the wall-clock minimum every login response (success
// or failure) is padded to (R-S-19). The Authenticate helper already burns
// a dummy bcrypt hash to equalise wrong-password vs unknown-username timing
// inside auth.Service, but the surrounding lockout-cache lookup, DB miss,
// audit persist, and totp-required dispatch each have their own latency
// signature. Padding every response to a fixed floor collapses the visible
// timing spread to <1ms regardless of which branch fired.
//
// 150ms is well above realistic local + cache paths (~5–20ms) and well
// below the user-perceived "slow" threshold so legitimate logins are not
// degraded.
//
// Kept as a package-level var so the test entrypoint (TestMain) can zero
// it out without every test file having to thread an explicit override
// through Options. Production callers never mutate it after init.
var loginTimingFloor = 150 * time.Millisecond

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

		// R-S-19: every response from this point on is padded to
		// loginTimingFloor before being written, so attackers cannot
		// distinguish lockout-cache hits from DB misses from real-auth
		// branches by wall-clock timing.
		start := s.now()
		floored := false
		ensureFloor := func() {
			if floored {
				return
			}
			floored = true
			elapsed := s.now().Sub(start)
			if remaining := loginTimingFloor - elapsed; remaining > 0 {
				time.Sleep(remaining)
			}
		}

		// Q2.U-S-15: serialise the entire IsLocked → verify → RecordFailure
		// sequence under a per-username shard lock. Without it, two
		// concurrent attempts on the same username can both pass the
		// IsLocked check, both run verify, and only one record a failure
		// — leaking an extra attempt past the lockout threshold.
		releaseAttempt := s.loginLockout.AttemptLock(request.Username)
		defer releaseAttempt()

		if s.loginLockout.IsLockedWithContext(r.Context(), request.Username, s.now()) {
			s.logger.Info("login attempt on locked account", "username_hash", s.logUsername(request.Username))
			ensureFloor()
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

		session, err := s.auth.AuthenticateWithContext(r.Context(), auth.LoginInput{
			Username:       request.Username,
			Password:       request.Password,
			TotpCode:       request.TotpCode,
			PriorSessionID: priorSessionID,
		}, s.now())
		if err != nil {
			// Record lockout-eligible failures: wrong password or wrong TOTP code.
			if errors.Is(err, auth.ErrInvalidCredentials) || errors.Is(err, auth.ErrInvalidTotpCode) {
				if s.loginLockout.CheckAndRecordFailureWithContext(r.Context(), request.Username, s.now()) {
					s.logger.Info("account locked out", "username_hash", s.logUsername(request.Username))
					ensureFloor()
					writeError(w, http.StatusUnauthorized, "account temporarily locked, try again later")
					return
				}
			}
			ensureFloor()
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
				s.logger.Error("session store unavailable during login", "username_hash", s.logUsername(request.Username), "error", err)
				writeErrorWithCode(w, http.StatusServiceUnavailable, "session store unavailable", "session_store_unavailable")
			default:
				s.logger.Error("auth login failed", "error", err)
				writeError(w, http.StatusInternalServerError, msgInternalError)
			}
			return
		}

		s.loginLockout.RecordSuccessWithContext(r.Context(), request.Username)
		s.logger.Info("user logged in", "username_hash", s.logUsername(request.Username), "user_id", session.UserID, "session_id", session.ID)

		// B1: persist the login audit event BEFORE issuing the session
		// cookie. Handing out a session cookie without a durable audit
		// record means a later incident response cannot attribute the
		// session to this login. If the storage write fails we revoke
		// the freshly created session and return 503 so the client
		// retries — no untraceable session is left alive.
		auditCtx, auditCancel := context.WithTimeout(r.Context(), loginAuditPersistTimeout)
		auditErr := s.appendAuditSync(auditCtx, session.UserID, "auth.login", session.ID, map[string]any{
			"username": request.Username,
		})
		auditCancel()
		if auditErr != nil {
			if logoutErr := s.auth.LogoutWithContext(r.Context(), session.ID); logoutErr != nil {
				s.logger.Error("failed to revoke session after audit persist failure",
					"session_id", session.ID, "error", logoutErr)
			}
			ensureFloor()
			writeErrorWithCode(w,
				http.StatusServiceUnavailable,
				"audit log unavailable, please retry",
				"audit_persist_unavailable",
			)
			return
		}

		ensureFloor()
		http.SetCookie(w, &http.Cookie{
			Name:     sessionCookieName,
			Value:    session.ID,
			Path:     "/",
			HttpOnly: true,
			SameSite: http.SameSiteStrictMode,
			Secure:   s.sessionCookieSecure(r),
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

		if err := s.auth.LogoutWithContext(r.Context(), session.ID); err != nil {
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

		secret, err := s.auth.StartTotpSetupWithContext(r.Context(), user.ID, s.now())
		if err != nil {
			s.logger.Error("start totp setup failed", "user_id", user.ID, "error", err)
			writeError(w, http.StatusInternalServerError, msgInternalError)
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

		updatedUser, err := s.auth.EnableTotpWithContext(r.Context(), user.ID, request.Password, request.TotpCode, s.now())
		if err != nil {
			switch {
			case errors.Is(err, auth.ErrInvalidCredentials), errors.Is(err, auth.ErrTotpRequired), errors.Is(err, auth.ErrInvalidTotpCode):
				writeError(w, http.StatusUnauthorized, err.Error())
			case errors.Is(err, auth.ErrTotpSetupNotFound):
				writeError(w, http.StatusBadRequest, err.Error())
			default:
				s.logger.Error("enable totp failed", "user_id", user.ID, "error", err)
				writeError(w, http.StatusInternalServerError, msgInternalError)
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

		updatedUser, err := s.auth.DisableTotpWithContext(r.Context(), user.ID, request.Password, request.TotpCode, s.now())
		if err != nil {
			switch {
			case errors.Is(err, auth.ErrInvalidCredentials), errors.Is(err, auth.ErrTotpRequired), errors.Is(err, auth.ErrInvalidTotpCode):
				writeError(w, http.StatusUnauthorized, err.Error())
			default:
				s.logger.Error("disable totp failed", "user_id", user.ID, "error", err)
				writeError(w, http.StatusInternalServerError, msgInternalError)
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

	user, err := s.auth.GetUserByIDWithContext(r.Context(), session.UserID)
	if err != nil {
		return auth.Session{}, auth.User{}, err
	}

	// S5: slide the idle-timeout forward. Internally throttled so a burst
	// of authenticated requests does not churn the session map. Cheap
	// enough (one map read + conditional write under the auth mutex) to
	// run on every authenticated handler.
	s.auth.TouchSessionWithContext(r.Context(), session.ID)

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

// EnvForceSecureCookie unconditionally marks the session cookie Secure,
// regardless of TLS heuristics (Q3.U-S-13). Operators set this in any
// production deployment fronted by HTTPS so a misconfigured proxy or a
// missing X-Forwarded-Proto cannot leak the cookie over plain HTTP.
const EnvForceSecureCookie = "PANVEX_FORCE_SECURE_COOKIE"

func (s *Server) sessionCookieSecure(r *http.Request) bool {
	// Q3.U-S-13: hardline override for prod. Setting
	// PANVEX_FORCE_SECURE_COOKIE=1 forces the Secure flag everywhere; if
	// the operator's deployment is actually plain HTTP, the cookie just
	// fails to ride along — better than silently leaking it.
	if forceCookieSecureEnabled() {
		return true
	}

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

func forceCookieSecureEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvForceSecureCookie))) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}
