package settings

import (
	"reflect"
	"testing"
)

type sampleBootstrap struct {
	HTTPListenAddress string `setting:"name=http.listen_address, type=hostport, default=:8080, env=PANVEX_HTTP_ADDR, toml=http.listen_address, apply=restart, desc='HTTP bind address.'"`
	StorageDriver     string `setting:"name=storage.driver, type=enum, values=sqlite|postgres, default=sqlite, env=PANVEX_STORAGE_DRIVER, toml=storage.driver, apply=config, desc='Storage driver.'"`
}

type sampleOperational struct {
	PasswordMinLength int `setting:"name=auth.password_min_length, type=int, default=10, min=8, max=64, apply=live, store=panel_settings.password_min_length, desc='Min password length.'"`
}

func TestWalkRegistry_Bootstrap(t *testing.T) {
	got, err := walkRegistry(reflect.TypeOf(sampleBootstrap{}), ClassBootstrap)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0].Name != "http.listen_address" || got[0].Class != ClassBootstrap || got[0].GoField != "HTTPListenAddress" {
		t.Fatalf("first field wrong: %+v", got[0])
	}
	if got[1].Name != "storage.driver" || len(got[1].Values) != 2 {
		t.Fatalf("second field wrong: %+v", got[1])
	}
}

func TestWalkRegistry_Operational(t *testing.T) {
	got, err := walkRegistry(reflect.TypeOf(sampleOperational{}), ClassOperational)
	if err != nil {
		t.Fatal(err)
	}
	if got[0].Class != ClassOperational || got[0].Store != "panel_settings.password_min_length" {
		t.Fatalf("operational missing store: %+v", got[0])
	}
}

func TestWalkRegistry_RejectMissingTag(t *testing.T) {
	type bad struct {
		Foo string
	}
	_, err := walkRegistry(reflect.TypeOf(bad{}), ClassBootstrap)
	if err == nil {
		t.Fatal("expected error for field without setting tag")
	}
}

func TestAllFields_Order(t *testing.T) {
	all := AllFields()
	if len(all) < 10 {
		t.Fatalf("AllFields too short: %d", len(all))
	}
	// bootstrap entries come first
	bsCount := 0
	for _, f := range all {
		if f.Class == ClassBootstrap {
			bsCount++
		} else {
			break
		}
	}
	for _, f := range all[bsCount:] {
		if f.Class != ClassOperational {
			t.Fatalf("after bootstrap, expected operational, got %q for %s", f.Class, f.Name)
		}
	}
}
