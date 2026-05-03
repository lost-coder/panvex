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

// resolveLoginTimingFloor picks the runtime floor used by the login
// handler. The zero value (Options.LoginTimingFloor unset) falls back
// to the production default; any negative value is interpreted as
// "no floor" (the explicit test-mode opt-out); any positive value
// overrides the production default.
func resolveLoginTimingFloor(override time.Duration) time.Duration {
	if override == 0 {
		return defaultLoginTimingFloor
	}
	if override < 0 {
		return 0
	}
	return override
}

// defaultLoginTimingFloor is the production wall-clock minimum every
// login response (success or failure) is padded to (R-S-19). The
// Authenticate helper already burns a dummy bcrypt hash to equalise
// wrong-password vs unknown-username timing inside auth.Service, but
// the surrounding lockout-cache lookup, DB miss, audit persist, and
// totp-required dispatch each have their own latency signature.
// Padding every response to a fixed floor collapses the visible
// timing spread to <1ms regardless of which branch fired.
//
// 150ms is well above realistic local + cache paths (~5–20ms) and well
// below the user-perceived "slow" threshold so legitimate logins are
// not degraded.
//
// The actual floor used at runtime is server.Server.loginTimingFloor;
// tests pass Options{LoginTimingFloor: 0} to skip the pad.
const defaultLoginTimingFloor = 150 * time.Millisecond

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
		ensureFloor := s.newLoginTimingFloor()

		// Task 6 (S-medium): IP-keyed lockout runs BEFORE the username
		// check so a locked IP can be rejected without our response
		// revealing whether the username exists or is itself locked. The
		// generic 401 message matches the username-locked branch below.
		clientIP := s.requestClientRateLimitKey(r)
		if s.ipLockout.IsLockedWithContext(r.Context(), clientIP, s.now()) {
			s.logger.Info("login attempt from locked ip", "ip_hash", logIPHash(clientIP))
			ensureFloor()
			writeError(w, http.StatusTooManyRequests, "too many attempts; try again later")
			return
		}

		// Q2.U-S-15: serialise the entire IsLocked → verify → RecordFailure
		// sequence under a per-username shard lock. Without it, two
		// concurrent attempts on the same username can both pass the
		// IsLocked check, both run verify, and only one record a failure
		// — leaking an extra attempt past the lockout threshold.
		releaseAttempt := s.loginLockout.AttemptLock(request.Username)
		defer releaseAttempt()

		// S-6: serialise the TOTP lockout window the same way. The two
		// trackers are independent counters (password vs second factor),
		// so each needs its own per-username shard lock to close the same
		// IsLocked → verify → RecordFailure race on its own counter.
		releaseTotpAttempt := s.totpLockout.AttemptLock(request.Username)
		defer releaseTotpAttempt()

		if s.loginLockout.IsLockedWithContext(r.Context(), request.Username, s.now()) {
			s.logger.Info("login attempt on locked account", "username_hash", s.logUsername(request.Username))
			ensureFloor()
			writeError(w, http.StatusUnauthorized, "account temporarily locked, try again later")
			return
		}

		// S-6: also reject if the second-factor counter is locked. This
		// returns the same generic "temporarily locked" message as the
		// password lockout so an attacker cannot tell which counter
		// tripped (and therefore cannot infer that the password is right).
		if s.totpLockout.IsLockedWithContext(r.Context(), request.Username, s.now()) {
			s.logger.Info("login attempt on totp-locked account", "username_hash", s.logUsername(request.Username))
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
		if existing := readSessionCookie(r); existing != "" {
			priorSessionID = existing
		}

		session, err := s.auth.Authenticate(r.Context(), auth.LoginInput{
			Username:       request.Username,
			Password:       request.Password,
			TotpCode:       request.TotpCode,
			PriorSessionID: priorSessionID,
		}, s.now())
		if err != nil {
			s.handleLoginAuthError(w, r, request.Username, err, ensureFloor)
			return
		}

		s.loginLockout.RecordSuccessWithContext(r.Context(), request.Username)
		// S-6: a fully successful login proves the user controls both the
		// password and the second factor (or that TOTP is disabled), so
		// clear the TOTP failure counter too. Symmetric with the password
		// reset above.
		s.totpLockout.RecordSuccessWithContext(r.Context(), request.Username)
		s.logger.Info("user logged in", "username_hash", s.logUsername(request.Username), "user_id", session.UserID, "session_hash", s.logSessionID(session.ID))

		if !s.persistLoginAudit(w, r, session, request.Username, ensureFloor) {
			return
		}

		ensureFloor()
		secure := s.sessionCookieSecure(r)
		http.SetCookie(w, &http.Cookie{
			// __Host- prefix when Secure: browser enforces Path=/, Secure,
			// no Domain attribute. Falls back to bare name in plain-HTTP
			// dev where Secure must be false. (Q3.U-S-13 / S-22.)
			Name:     sessionCookieNameFor(secure),
			Value:    session.ID,
			Path:     "/",
			HttpOnly: true,
			SameSite: http.SameSiteStrictMode,
			Secure:   secure,
		})

		writeJSON(w, http.StatusOK, map[string]string{
			"status": "ok",
		})
	}
}

// newLoginTimingFloor returns a closure that ensures any login response is
// delayed at least s.loginTimingFloor since the call site started. The
// closure is idempotent — multiple calls within one request only sleep
// once.
func (s *Server) newLoginTimingFloor() func() {
	start := s.now()
	floored := false
	return func() {
		if floored {
			return
		}
		floored = true
		elapsed := s.now().Sub(start)
		if remaining := s.loginTimingFloor - elapsed; remaining > 0 {
			time.Sleep(remaining)
		}
	}
}

// handleLoginAuthError records lockout-eligible failures and writes the
// appropriate HTTP response for a failed AuthenticateWithContext call.
//
// S-6: password failures and TOTP failures feed independent counters.
// The password tracker is the lenient one (LockoutMaxAttempts /
// LockoutDuration) so legitimate users who fat-finger a password don't
// get locked out instantly. The TOTP tracker is the stricter one
// (TOTPLockoutMaxAttempts / TOTPLockoutDuration) because by the time an
// attempt reaches the TOTP step the attacker has already proven they
// hold the password — at that point a 6-digit code is the only
// remaining defence and should not share the password budget.
func (s *Server) handleLoginAuthError(w http.ResponseWriter, r *http.Request, username string, err error, ensureFloor func()) {
	// Task 6 (S-medium): bump the IP-keyed counter on any auth-style
	// failure (bad password OR bad TOTP). The username-keyed counters
	// run independently below; we still record per-username so the
	// existing single-account lockout semantics are preserved.
	clientIP := s.requestClientRateLimitKey(r)
	switch {
	case errors.Is(err, auth.ErrInvalidCredentials), errors.Is(err, auth.ErrInvalidTotpCode):
		if s.ipLockout.CheckAndRecordFailureWithContext(r.Context(), clientIP, s.now()) {
			// IP was already locked at decision time. Same generic
			// 429 the pre-username gate returns so an attacker
			// cannot tell which counter tripped.
			s.logger.Info("login failure from locked ip", "ip_hash", logIPHash(clientIP))
			ensureFloor()
			writeError(w, http.StatusTooManyRequests, "too many attempts; try again later")
			return
		}
	}

	switch {
	case errors.Is(err, auth.ErrInvalidCredentials):
		if s.loginLockout.CheckAndRecordFailureWithContext(r.Context(), username, s.now()) {
			s.logger.Info("account locked out", "username_hash", s.logUsername(username))
			ensureFloor()
			writeError(w, http.StatusUnauthorized, "account temporarily locked, try again later")
			return
		}
	case errors.Is(err, auth.ErrInvalidTotpCode):
		if s.totpLockout.CheckAndRecordFailureWithContext(r.Context(), username, s.now()) {
			s.logger.Info("account totp locked out", "username_hash", s.logUsername(username))
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
		s.logger.Error("session store unavailable during login", "username_hash", s.logUsername(username), "error", err)
		writeErrorWithCode(w, http.StatusServiceUnavailable, "session store unavailable", "session_store_unavailable")
	default:
		s.logger.Error("auth login failed", "error", err)
		writeError(w, http.StatusInternalServerError, msgInternalError)
	}
}

// persistLoginAudit writes the auth.login audit event synchronously and, on
// failure, revokes the freshly issued session and emits the 503 response.
// Returns true if persistence succeeded and the caller may proceed to issue
// the cookie. Implements B1.
func (s *Server) persistLoginAudit(w http.ResponseWriter, r *http.Request, session auth.Session, username string, ensureFloor func()) bool {
	auditCtx, auditCancel := context.WithTimeout(r.Context(), loginAuditPersistTimeout)
	auditErr := s.appendAuditSync(auditCtx, session.UserID, "auth.login", s.logSessionID(session.ID), map[string]any{
		// L-4: redact username via the same per-process HMAC the rest
		// of the audit/log pipeline uses; raw usernames may be PII
		// (operator email addresses) and the audit log is read by
		// every operator, not just admins.
		"username_hash": s.logUsername(username),
	})
	auditCancel()
	if auditErr == nil {
		return true
	}
	if logoutErr := s.auth.Logout(r.Context(), session.ID); logoutErr != nil {
		s.logger.Error("failed to revoke session after audit persist failure",
			"session_hash", s.logSessionID(session.ID), "error", logoutErr)
	}
	ensureFloor()
	writeErrorWithCode(w,
		http.StatusServiceUnavailable,
		"audit log unavailable, please retry",
		"audit_persist_unavailable",
	)
	return false
}

func (s *Server) handleLogout() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, _, err := s.requireSession(r)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		if err := s.auth.Logout(r.Context(), session.ID); err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		s.logger.Info("user logged out", "user_id", session.UserID, "session_hash", s.logSessionID(session.ID))
		secure := s.sessionCookieSecure(r)
		// Expire BOTH cookie names so a deployment that toggles Secure (or a
		// browser that still holds a stale variant) cannot retain a logged-out
		// session under the other name.
		http.SetCookie(w, &http.Cookie{
			Name:     sessionCookieNameHostPrefix,
			Value:    "",
			Path:     "/",
			HttpOnly: true,
			MaxAge:   -1,
			SameSite: http.SameSiteStrictMode,
			Secure:   true,
		})
		http.SetCookie(w, &http.Cookie{
			Name:     sessionCookieName,
			Value:    "",
			Path:     "/",
			HttpOnly: true,
			MaxAge:   -1,
			SameSite: http.SameSiteStrictMode,
			Secure:   secure,
		})
		s.appendAuditWithContext(r.Context(), session.UserID, "auth.logout", s.logSessionID(session.ID), nil)

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

		secret, err := s.auth.StartTotpSetup(r.Context(), user.ID, s.now())
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

		updatedUser, err := s.auth.EnableTotp(r.Context(), user.ID, request.Password, request.TotpCode, s.now())
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

		updatedUser, err := s.auth.DisableTotp(r.Context(), user.ID, request.Password, request.TotpCode, s.now())
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

	value := readSessionCookie(r)
	if value == "" {
		return auth.Session{}, auth.User{}, http.ErrNoCookie
	}

	session, err := s.auth.GetSession(value)
	if err != nil {
		return auth.Session{}, auth.User{}, err
	}

	user, err := s.auth.GetUserByID(r.Context(), session.UserID)
	if err != nil {
		return auth.Session{}, auth.User{}, err
	}

	// S5: slide the idle-timeout forward. Internally throttled so a burst
	// of authenticated requests does not churn the session map. Cheap
	// enough (one map read + conditional write under the auth mutex) to
	// run on every authenticated handler.
	s.auth.TouchSession(r.Context(), session.ID)

	return session, user, nil
}

func buildTotpAuthURL(username, secret string) string {
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

// sessionCookieNameFor returns the cookie name to emit on Set-Cookie based on
// whether the cookie will be marked Secure. The __Host- prefix is only valid
// when Secure=true and Path=/ and Domain is unset; we satisfy the latter two
// unconditionally and gate the prefix on the Secure decision so dev/loopback
// clients (where Secure is false) still receive a usable cookie. (S-22.)
func sessionCookieNameFor(secure bool) string {
	if secure {
		return sessionCookieNameHostPrefix
	}
	return sessionCookieName
}

// readSessionCookie returns the session ID carried by either the host-prefix
// cookie (preferred — production) or the bare cookie (dev/loopback). When
// both are present the host-prefix variant wins, since it is the variant a
// modern Secure deployment would have just issued. Returns "" if neither is
// present.
func readSessionCookie(r *http.Request) string {
	if c, err := r.Cookie(sessionCookieNameHostPrefix); err == nil && c.Value != "" {
		return c.Value
	}
	if c, err := r.Cookie(sessionCookieName); err == nil && c.Value != "" {
		return c.Value
	}
	return ""
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
