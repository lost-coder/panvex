package agenttransport

import (
	"context"
	"errors"
	"strings"

	"github.com/lost-coder/panvex/internal/controlplane/enrollment"
)

// classifyDialError maps a network/TLS/RPC error returned by the outbound
// dial path to an operator-friendly enrollment.ErrorCode. The string
// matching is intentionally coarse — Phase 2 may wrap sentinel errors at
// each call site for typed dispatch.
func classifyDialError(err error) enrollment.ErrorCode {
	if err == nil {
		return enrollment.ErrInternal
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return enrollment.ErrOutboundDialTimeout
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "cert pin mismatch"):
		return enrollment.ErrTLSPinMismatch
	case strings.Contains(msg, "connection refused"):
		return enrollment.ErrOutboundListenerRefused
	case strings.Contains(msg, "i/o timeout"):
		return enrollment.ErrOutboundDialTimeout
	case strings.Contains(msg, "no such host"), strings.Contains(msg, "network is unreachable"):
		return enrollment.ErrPanelUnreachable
	default:
		return enrollment.ErrInternal
	}
}
