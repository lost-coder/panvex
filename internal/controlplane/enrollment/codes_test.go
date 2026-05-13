package enrollment

import "testing"

func TestErrorCodeRegistryComplete(t *testing.T) {
	want := []ErrorCode{
		ErrTokenExpired,
		ErrTokenAlreadyUsed,
		ErrTokenNotFound,
		ErrTLSPinMismatch,
		ErrPanelUnreachable,
		ErrCSRInvalid,
		ErrCSRSubjectMismatch,
		ErrCertSignFailed,
		ErrOutboundDialTimeout,
		ErrOutboundListenerRefused,
		ErrInternal,
	}
	for _, code := range want {
		msg, ok := MessageFor(code)
		if !ok {
			t.Fatalf("code %q has no registry entry", code)
		}
		if msg == "" {
			t.Fatalf("code %q has empty message", code)
		}
	}
}

func TestMessageForUnknownCode(t *testing.T) {
	if _, ok := MessageFor("DOES_NOT_EXIST"); ok {
		t.Fatalf("expected ok=false for unknown code")
	}
}
