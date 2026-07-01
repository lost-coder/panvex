package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lost-coder/panvex/internal/controlplane/jobs"
	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

// TestHandleClientMutationErrorAllowlist guards the 3.10 fix:
// handleClientMutationError must map every recognised sentinel to its fixed
// msg* constant and must NEVER echo the raw wrapped err.Error() text, even
// when that text happens to contain extra detail appended by a caller (e.g.
// fmt.Errorf("...: %w", sentinel)). Before the fix this handler wrote
// err.Error() verbatim for not-found/validation/conflict errors, so any
// detail an intermediate layer wrapped onto the sentinel would leak straight
// into the HTTP response body.
func TestHandleClientMutationErrorAllowlist(t *testing.T) {
	const leakedDetail = "row_id=internal-42 dsn=postgres://leaked"

	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantBody   string
	}{
		{
			name:       "not found",
			err:        fmt.Errorf("lookup client %s: %w", leakedDetail, storage.ErrNotFound),
			wantStatus: http.StatusNotFound,
			wantBody:   msgClientNotFound,
		},
		{
			name:       "name required",
			err:        fmt.Errorf("validate %s: %w", leakedDetail, errClientNameRequired),
			wantStatus: http.StatusBadRequest,
			wantBody:   msgClientNameRequired,
		},
		{
			name:       "name invalid",
			err:        fmt.Errorf("validate %s: %w", leakedDetail, errClientNameInvalid),
			wantStatus: http.StatusBadRequest,
			wantBody:   msgClientNameInvalid,
		},
		{
			name:       "user ad tag",
			err:        fmt.Errorf("validate %s: %w", leakedDetail, errClientUserADTag),
			wantStatus: http.StatusBadRequest,
			wantBody:   msgClientUserADTag,
		},
		{
			name:       "expiration",
			err:        fmt.Errorf("validate %s: %w", leakedDetail, errClientExpiration),
			wantStatus: http.StatusBadRequest,
			wantBody:   msgClientExpiration,
		},
		{
			name:       "targets required",
			err:        fmt.Errorf("validate %s: %w", leakedDetail, errClientTargetsRequired),
			wantStatus: http.StatusBadRequest,
			wantBody:   msgClientTargetsRequired,
		},
		{
			name:       "limit negative",
			err:        fmt.Errorf("validate %s: %w", leakedDetail, errClientLimitNegative),
			wantStatus: http.StatusBadRequest,
			wantBody:   msgClientLimitNegative,
		},
		{
			name:       "read only target",
			err:        fmt.Errorf("dispatch %s: %w", leakedDetail, jobs.ErrReadOnlyTarget),
			wantStatus: http.StatusConflict,
			wantBody:   msgClientReadOnlyTarget,
		},
		{
			name:       "unrecognised operational error falls through to generic 500",
			err:        fmt.Errorf("db exploded: %s", leakedDetail),
			wantStatus: http.StatusInternalServerError,
			wantBody:   msgInternalError,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			handled := handleClientMutationError(rec, tc.err)
			if handled {
				t.Fatalf("handleClientMutationError returned true (no error written) for %v", tc.err)
			}
			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tc.wantStatus)
			}
			var body errorResponse
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("unmarshal response body: %v", err)
			}
			if body.Error != tc.wantBody {
				t.Fatalf("body.Error = %q, want %q", body.Error, tc.wantBody)
			}
			if strings.Contains(rec.Body.String(), leakedDetail) {
				t.Fatalf("response body leaked raw error detail: %s", rec.Body.String())
			}
		})
	}
}

// TestHandleClientMutationErrorNilIsNoop confirms the nil fast-path is
// unaffected by the allowlist rewrite.
func TestHandleClientMutationErrorNilIsNoop(t *testing.T) {
	rec := httptest.NewRecorder()
	if !handleClientMutationError(rec, nil) {
		t.Fatal("expected true for nil error")
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("expected no response body written, got: %s", rec.Body.String())
	}
}
