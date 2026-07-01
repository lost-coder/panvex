package updatehosts

import (
	"strings"
	"testing"
)

func TestPolicyFromEnvDefault(t *testing.T) {
	t.Setenv(EnvAllowedHosts, "")
	p := PolicyFromEnv()
	if p.Disabled() {
		t.Fatal("empty env must not disable the allow-list")
	}
	if err := p.CheckURL("https://github.com/o/r/releases"); err != nil {
		t.Fatalf("default GitHub host rejected: %v", err)
	}
	if err := p.CheckURL("https://release-assets.githubusercontent.com/x"); err != nil {
		t.Fatalf("release-assets host rejected: %v", err)
	}
	if err := p.CheckURL("https://evil.example.com/x"); err == nil {
		t.Fatal("off-list host accepted under default policy")
	} else if !strings.Contains(err.Error(), "not in the allow-list") {
		t.Fatalf("error = %q, want it to mention the allow-list", err)
	}
}

func TestPolicyFromEnvWildcardDisables(t *testing.T) {
	t.Setenv(EnvAllowedHosts, "*")
	p := PolicyFromEnv()
	if !p.Disabled() {
		t.Fatal("'*' must disable the host allow-list")
	}
	if err := p.CheckURL("https://anything.example.com/x"); err != nil {
		t.Fatalf("disabled policy rejected an https host: %v", err)
	}
	if err := p.CheckURL("http://anything.example.com/x"); err == nil {
		t.Fatal("disabled policy accepted http — https must still be enforced")
	}
}

func TestPolicyFromEnvExplicitList(t *testing.T) {
	t.Setenv(EnvAllowedHosts, " mirror.internal , github.com ")
	p := PolicyFromEnv()
	if p.Disabled() {
		t.Fatal("explicit list must not disable")
	}
	if err := p.CheckURL("https://mirror.internal/x"); err != nil {
		t.Fatalf("listed mirror rejected: %v", err)
	}
	if err := p.CheckURL("https://objects.githubusercontent.com/x"); err == nil {
		t.Fatal("host outside the explicit list was accepted")
	}
}

func TestCheckURLRejectsNonHTTPS(t *testing.T) {
	t.Setenv(EnvAllowedHosts, "")
	p := PolicyFromEnv()
	if err := p.CheckURL("http://github.com/x"); err == nil {
		t.Fatal("http URL accepted")
	}
}

func TestIsDefaultHost(t *testing.T) {
	if !IsDefaultHost("release-assets.githubusercontent.com") {
		t.Fatal("release-assets must be a default host")
	}
	if IsDefaultHost("mirror.internal") {
		t.Fatal("mirror.internal must not be a default host")
	}
}
