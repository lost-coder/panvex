package geoip_test

import (
	"encoding/json"
	"testing"

	"github.com/lost-coder/panvex/internal/controlplane/geoip"
)

func TestSettingsModeConstants(t *testing.T) {
	if geoip.ModeDisabled != "" {
		t.Errorf("ModeDisabled = %q, want \"\"", geoip.ModeDisabled)
	}
	if geoip.ModeAuto != "auto" || geoip.ModeURL != "url" || geoip.ModeLocal != "local" {
		t.Errorf("mode constants drift: auto=%q url=%q local=%q",
			geoip.ModeAuto, geoip.ModeURL, geoip.ModeLocal)
	}
}

func TestSettingsRoundTripJSON(t *testing.T) {
	in := geoip.Settings{
		Mode: geoip.ModeURL,
		City: geoip.Source{Enabled: true, URL: "https://example.com/city.mmdb"},
		ASN:  geoip.Source{Enabled: false, URL: "https://example.com/asn.mmdb"},
	}
	data, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out geoip.Settings
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out != in {
		t.Errorf("round-trip mismatch:\n got %#v\nwant %#v", out, in)
	}
}

func TestSourceForKind(t *testing.T) {
	s := geoip.Settings{
		City: geoip.Source{Enabled: true, URL: "city"},
		ASN:  geoip.Source{Enabled: false, URL: "asn"},
	}
	if got := s.SourceFor(geoip.KindCity).URL; got != "city" {
		t.Errorf("SourceFor(City).URL = %q, want city", got)
	}
	if got := s.SourceFor(geoip.KindASN).URL; got != "asn" {
		t.Errorf("SourceFor(ASN).URL = %q, want asn", got)
	}
}
