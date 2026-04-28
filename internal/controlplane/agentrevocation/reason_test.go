package agentrevocation

import (
	"errors"
	"testing"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestRevokedStatusCarriesAgentRevokedDetail(t *testing.T) {
	st := RevokedStatus("agent certificate has been revoked")
	if st.Code() != codes.PermissionDenied {
		t.Fatalf("status code = %s, want PermissionDenied", st.Code())
	}
	var info *errdetails.ErrorInfo
	for _, d := range st.Details() {
		if e, ok := d.(*errdetails.ErrorInfo); ok {
			info = e
			break
		}
	}
	if info == nil {
		t.Fatal("RevokedStatus did not attach an ErrorInfo detail")
	}
	if info.Reason != Reason {
		t.Fatalf("ErrorInfo.Reason = %q, want %q", info.Reason, Reason)
	}
	if info.Domain != Domain {
		t.Fatalf("ErrorInfo.Domain = %q, want %q", info.Domain, Domain)
	}
}

func TestIsAgentRevokedMatchesStructuredStatus(t *testing.T) {
	err := RevokedStatus("agent certificate has been revoked").Err()
	if !IsAgentRevoked(err) {
		t.Fatal("IsAgentRevoked() = false, want true for RevokedStatus().Err()")
	}
}

func TestIsAgentRevokedRejectsBareStatusWithoutDetail(t *testing.T) {
	err := status.Error(codes.PermissionDenied, "agent certificate has been revoked")
	if IsAgentRevoked(err) {
		t.Fatal("IsAgentRevoked() = true for bare PermissionDenied status, want false")
	}
}

func TestIsAgentRevokedRejectsOtherCodes(t *testing.T) {
	err := status.Error(codes.Unavailable, "service unavailable")
	if IsAgentRevoked(err) {
		t.Fatal("IsAgentRevoked() = true for Unavailable, want false")
	}
}

func TestIsAgentRevokedRejectsForeignReason(t *testing.T) {
	st := status.New(codes.PermissionDenied, "other policy")
	withDetails, err := st.WithDetails(&errdetails.ErrorInfo{
		Reason: "SOMETHING_ELSE",
		Domain: Domain,
	})
	if err != nil {
		t.Fatalf("WithDetails() error = %v", err)
	}
	if IsAgentRevoked(withDetails.Err()) {
		t.Fatal("IsAgentRevoked() = true for foreign Reason, want false")
	}
}

func TestIsAgentRevokedRejectsForeignDomain(t *testing.T) {
	st := status.New(codes.PermissionDenied, "other policy")
	withDetails, err := st.WithDetails(&errdetails.ErrorInfo{
		Reason: Reason,
		Domain: "example.com",
	})
	if err != nil {
		t.Fatalf("WithDetails() error = %v", err)
	}
	if IsAgentRevoked(withDetails.Err()) {
		t.Fatal("IsAgentRevoked() = true for foreign Domain, want false")
	}
}

func TestIsAgentRevokedHandlesNilAndPlainErrors(t *testing.T) {
	if IsAgentRevoked(nil) {
		t.Fatal("IsAgentRevoked(nil) = true, want false")
	}
	if IsAgentRevoked(errors.New("boom")) {
		t.Fatal("IsAgentRevoked(plain error) = true, want false")
	}
}
