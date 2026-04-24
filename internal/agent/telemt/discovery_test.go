package telemt

import "testing"

// TestExtractSecretFromLinks_TLSFakeTLS locks in the fake-TLS secret
// layout: the ee-prefixed parameter is HEX32-then-domain_hex, not the
// other way around. Getting this wrong causes every user on a given
// node to report the same domain-tail as their "secret", which blows
// up the same_secret_different_names conflict detector.
func TestExtractSecretFromLinks_TLSFakeTLS(t *testing.T) {
	const wantSecret = "2ed9f5e073e33b8838e3c55ee9ae0579"
	// ee + wantSecret + hex("ds87j.metrion.icu")
	link := "tg://proxy?server=ds87j.metrion.icu&port=443&secret=ee" +
		wantSecret + "647338376a2e6d657472696f6e2e696375"

	got := extractSecretFromLinks(UserLinks{TLS: []string{link}})
	if got != wantSecret {
		t.Fatalf("extractSecretFromLinks: got %q, want %q", got, wantSecret)
	}
}

// TestExtractSecretFromLinks_Classic keeps the classic path honest —
// the secret= param is the raw 32-hex value with no prefix.
func TestExtractSecretFromLinks_Classic(t *testing.T) {
	const wantSecret = "abcdef0123456789abcdef0123456789"
	link := "tg://proxy?server=example.com&port=443&secret=" + wantSecret
	got := extractSecretFromLinks(UserLinks{Classic: []string{link}})
	if got != wantSecret {
		t.Fatalf("extractSecretFromLinks: got %q, want %q", got, wantSecret)
	}
}

// TestExtractSecretFromLinks_SecureDD locks the dd-prefix path: the
// raw secret sits after the "dd" obfuscation marker.
func TestExtractSecretFromLinks_SecureDD(t *testing.T) {
	const wantSecret = "deadbeefcafebabe0123456789abcdef"
	link := "tg://proxy?server=example.com&port=443&secret=dd" + wantSecret
	got := extractSecretFromLinks(UserLinks{Secure: []string{link}})
	if got != wantSecret {
		t.Fatalf("extractSecretFromLinks: got %q, want %q", got, wantSecret)
	}
}
