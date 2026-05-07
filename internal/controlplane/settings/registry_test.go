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

func TestRegistry_OperationalAllFieldsParse(t *testing.T) {
	fields, err := walkRegistry(reflect.TypeOf(Operational{}), ClassOperational)
	if err != nil {
		t.Fatalf("walkRegistry(Operational): %v", err)
	}
	if len(fields) == 0 {
		t.Fatal("Operational registry empty")
	}
	for _, f := range fields {
		if f.Class != ClassOperational {
			t.Fatalf("%s class = %q, want operational", f.Name, f.Class)
		}
		if f.Store == "" {
			t.Errorf("%s: operational must declare store=", f.Name)
		}
	}
}

func TestRegistry_AllNamesGloballyUnique(t *testing.T) {
	bf, _ := walkRegistry(reflect.TypeOf(Bootstrap{}), ClassBootstrap)
	of, _ := walkRegistry(reflect.TypeOf(Operational{}), ClassOperational)
	seen := map[string]bool{}
	for _, f := range append(bf, of...) {
		if seen[f.Name] {
			t.Fatalf("name %q appears in both Bootstrap and Operational", f.Name)
		}
		seen[f.Name] = true
	}
}
