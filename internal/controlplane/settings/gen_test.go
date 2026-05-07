package settings

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRenderSchemaJSON_ContainsKnownField(t *testing.T) {
	body, err := RenderSchemaJSON()
	if err != nil {
		t.Fatal(err)
	}
	var arr []map[string]any
	if err := json.Unmarshal(body, &arr); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	found := false
	for _, e := range arr {
		if e["name"] == "auth.encryption_key" {
			found = true
			if e["secret"] != true {
				t.Errorf("auth.encryption_key: secret should be true, got %#v", e["secret"])
			}
			if _, has := e["default"]; has {
				t.Errorf("auth.encryption_key: secret entry must not expose default")
			}
		}
	}
	if !found {
		t.Fatal("auth.encryption_key missing from schema")
	}
	if !strings.Contains(string(body), `"http.listen_address"`) {
		t.Fatal("http.listen_address missing")
	}
}

func TestRenderReferenceMarkdown(t *testing.T) {
	body, err := RenderReferenceMarkdown()
	if err != nil {
		t.Fatal(err)
	}
	s := string(body)
	for _, want := range []string{
		"# Panvex Settings Reference",
		"## Bootstrap settings",
		"## Operational settings",
		"`http.listen_address`",
		"`PANVEX_ENCRYPTION_KEY`",
		"`auth.password_min_length`",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("reference.md missing %q", want)
		}
	}
}

func TestRenderExampleConfigTOML(t *testing.T) {
	body, err := RenderExampleConfigTOML()
	if err != nil {
		t.Fatal(err)
	}
	s := string(body)
	if !strings.HasPrefix(s, "# Panvex control-plane example config") {
		t.Errorf("missing header in example.config.toml")
	}
	for _, sect := range []string{"[http]", "[grpc]", "[storage]", "[panel]"} {
		if !strings.Contains(s, sect) {
			t.Errorf("missing TOML section %q", sect)
		}
	}
	for _, omit := range []string{"PANVEX_ENCRYPTION_KEY", "auth.encryption_key"} {
		if strings.Contains(s, omit) {
			t.Errorf("example.config.toml leaked env-only entry %q", omit)
		}
	}
}
