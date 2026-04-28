// Package agentrevocation owns the wire-level contract used by the
// control-plane to tell a deregistered agent that it should stop
// reconnecting.
//
// The contract is a google.rpc.ErrorInfo attached to a gRPC
// PermissionDenied status. Carrying a structured detail (instead of
// matching on the human-readable status message) lets the agent reliably
// distinguish "you have been deregistered, give up" from other
// PermissionDenied causes (cert pin mismatch, agent_id confusion) where
// retrying still makes sense.
package agentrevocation

import (
	"errors"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Reason is the ErrorInfo.Reason value attached to PermissionDenied
// responses for agents whose record has been removed from the panel.
// Stable wire string — agents in the field key off this exact value.
const Reason = "AGENT_REVOKED"

// Domain is the ErrorInfo.Domain value, scoping the reason to this
// project so the agent does not confuse it with a same-named reason from
// another upstream service.
const Domain = "panvex.io"

// RevokedStatus returns a PermissionDenied gRPC status with an
// ErrorInfo{Reason: AGENT_REVOKED, Domain: panvex.io} detail attached.
// Callers turn it into an error via .Err(). If WithDetails fails (it
// only does for malformed proto messages), the bare status is returned
// — the agent then falls back to its conservative "retry forever"
// behaviour, which is safe.
func RevokedStatus(message string) *status.Status {
	st := status.New(codes.PermissionDenied, message)
	withDetails, err := st.WithDetails(&errdetails.ErrorInfo{
		Reason: Reason,
		Domain: Domain,
	})
	if err != nil {
		return st
	}
	return withDetails
}

// IsAgentRevoked reports whether err is a gRPC status carrying the
// AGENT_REVOKED ErrorInfo detail. Returns false for nil, non-gRPC, and
// gRPC errors without the detail.
func IsAgentRevoked(err error) bool {
	if err == nil {
		return false
	}
	st, ok := status.FromError(err)
	if !ok {
		return false
	}
	if st.Code() != codes.PermissionDenied {
		return false
	}
	for _, detail := range st.Details() {
		info, ok := detail.(*errdetails.ErrorInfo)
		if !ok {
			continue
		}
		if info.Reason == Reason && info.Domain == Domain {
			return true
		}
	}
	return false
}

// ErrAgentRevoked is the sentinel that agent runtime code surfaces up
// to main when IsAgentRevoked matches a transport error. main maps it
// to os.Exit(78) so systemd (configured with RestartPreventExitStatus=78
// by install-agent.sh) does not restart a zombie loop.
var ErrAgentRevoked = errors.New("agent has been deregistered on the panel")
