package updates

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func respWith(status int, body string, headers map[string]string) *http.Response {
	h := http.Header{}
	for k, v := range headers {
		h.Set(k, v)
	}
	return &http.Response{
		StatusCode: status,
		Header:     h,
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func TestGithubAPIError_RateLimitExhausted(t *testing.T) {
	resp := respWith(http.StatusForbidden,
		`{"message":"API rate limit exceeded for 1.2.3.4."}`,
		map[string]string{
			"X-RateLimit-Remaining": "0",
			"X-RateLimit-Limit":     "60",
			"X-RateLimit-Reset":     "1780093524",
		})
	err := githubAPIError(resp)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	msg := err.Error()
	for _, want := range []string{"rate limit", "60", "token"} {
		if !strings.Contains(strings.ToLower(msg), strings.ToLower(want)) {
			t.Fatalf("error %q missing %q", msg, want)
		}
	}
}

func TestGithubAPIError_GenericIncludesMessage(t *testing.T) {
	resp := respWith(http.StatusNotFound, `{"message":"Not Found"}`, nil)
	err := githubAPIError(resp)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, "404") || !strings.Contains(msg, "Not Found") {
		t.Fatalf("error %q should contain status 404 and the GitHub message", msg)
	}
}

func TestGithubAPIError_NoBodyStillReportsStatus(t *testing.T) {
	resp := respWith(http.StatusBadGateway, ``, nil)
	err := githubAPIError(resp)
	if err == nil || !strings.Contains(err.Error(), "502") {
		t.Fatalf("expected status 502 in error, got %v", err)
	}
}
