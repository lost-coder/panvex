package webhooks

import "testing"

func TestSignVerifyRoundTrip(t *testing.T) {
	secret := []byte("hush-hush")
	ts := []byte("2026-05-08T00:00:00Z")
	body := []byte(`{"action":"agent.unhealthy","agent":"a-1"}`)

	sig := Sign(secret, ts, body)
	if !Verify(secret, ts, body, sig) {
		t.Fatalf("Verify rejected a freshly Signed value")
	}
}

func TestVerifyRejectsTampering(t *testing.T) {
	secret := []byte("k")
	ts := []byte("2026-05-08T00:00:00Z")
	body := []byte(`{"a":1}`)
	sig := Sign(secret, ts, body)

	cases := []struct {
		name      string
		secret    []byte
		timestamp []byte
		body      []byte
	}{
		{"different secret", []byte("k2"), ts, body},
		{"different timestamp", secret, []byte("2026-05-08T00:00:01Z"), body},
		{"different body", secret, ts, []byte(`{"a":2}`)},
		{"truncated body", secret, ts, body[:len(body)-1]},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if Verify(c.secret, c.timestamp, c.body, sig) {
				t.Errorf("Verify accepted tampered input")
			}
		})
	}
}

func TestVerifyRejectsMalformedHeader(t *testing.T) {
	secret := []byte("k")
	ts := []byte("t")
	body := []byte("b")

	if Verify(secret, ts, body, "") {
		t.Error("empty header accepted")
	}
	if Verify(secret, ts, body, "deadbeef") {
		t.Error("hex without sha256= prefix accepted")
	}
	if Verify(secret, ts, body, "sha256=") {
		t.Error("sha256= with no hex accepted")
	}
}
