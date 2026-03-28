package server

import (
	"context"
	"net/http"

	"github.com/panvex/panvex/internal/controlplane/auth"
)

type requestAuthContextKey int

const (
	requestAuthSessionKey requestAuthContextKey = iota
	requestAuthUserKey
)

func (s *Server) requireAuthenticatedSession() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			session, user, ok := authenticateRequestSession(s, w, r)
			if !ok {
				return
			}
			next.ServeHTTP(w, withRequestAuthContext(r, session, user))
		})
	}
}

func (s *Server) requireMinimumRole(required auth.Role) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			session, user, ok := authenticateRequestSession(s, w, r)
			if !ok {
				return
			}
			if !roleSatisfies(user.Role, required) {
				writeError(w, http.StatusForbidden, forbiddenMessageForRole(required))
				return
			}
			next.ServeHTTP(w, withRequestAuthContext(r, session, user))
		})
	}
}

func authenticateRequestSession(s *Server, w http.ResponseWriter, r *http.Request) (auth.Session, auth.User, bool) {
	if session, user, ok := requestAuthContext(r); ok {
		return session, user, true
	}

	session, user, err := s.requireSession(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return auth.Session{}, auth.User{}, false
	}

	return session, user, true
}

func withRequestAuthContext(r *http.Request, session auth.Session, user auth.User) *http.Request {
	ctx := context.WithValue(r.Context(), requestAuthSessionKey, session)
	ctx = context.WithValue(ctx, requestAuthUserKey, user)
	return r.WithContext(ctx)
}

func requestAuthContext(r *http.Request) (auth.Session, auth.User, bool) {
	sessionValue := r.Context().Value(requestAuthSessionKey)
	userValue := r.Context().Value(requestAuthUserKey)
	if sessionValue == nil || userValue == nil {
		return auth.Session{}, auth.User{}, false
	}

	session, sessionOK := sessionValue.(auth.Session)
	user, userOK := userValue.(auth.User)
	if !sessionOK || !userOK {
		return auth.Session{}, auth.User{}, false
	}

	return session, user, true
}

func roleSatisfies(current auth.Role, required auth.Role) bool {
	return roleRank(current) >= roleRank(required)
}

func roleRank(role auth.Role) int {
	switch role {
	case auth.RoleAdmin:
		return 3
	case auth.RoleOperator:
		return 2
	case auth.RoleViewer:
		return 1
	default:
		return 0
	}
}

func forbiddenMessageForRole(required auth.Role) string {
	if required == auth.RoleAdmin {
		return "admin role required"
	}

	return "viewer role cannot access this endpoint"
}
