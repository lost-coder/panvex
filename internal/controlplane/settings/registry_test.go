package settings

import (
	"reflect"
	"testing"
)

func TestRegistry_BootstrapAllFieldsParse(t *testing.T) {
	fields, err := walkRegistry(reflect.TypeOf(Bootstrap{}), ClassBootstrap)
	if err != nil {
		t.Fatalf("walkRegistry(Bootstrap): %v", err)
	}
	if len(fields) == 0 {
		t.Fatal("Bootstrap registry empty")
	}
	seen := map[string]bool{}
	for _, f := range fields {
		if seen[f.Name] {
			t.Fatalf("duplicate setting name %q", f.Name)
		}
		seen[f.Name] = true
		if f.Class != ClassBootstrap {
			t.Fatalf("%s class = %q, want bootstrap", f.Name, f.Class)
		}
	}
}

func TestRegistry_BootstrapHasRequiredFields(t *testing.T) {
	fields, _ := walkRegistry(reflect.TypeOf(Bootstrap{}), ClassBootstrap)
	want := []string{
		"http.listen_address", "grpc.listen_address",
		"storage.driver", "storage.dsn",
		"auth.encryption_key",
		"observability.log_level",
	}
	have := map[string]bool{}
	for _, f := range fields {
		have[f.Name] = true
	}
	for _, w := range want {
		if !have[w] {
			t.Errorf("missing required setting %q", w)
		}
	}
}
