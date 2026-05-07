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
