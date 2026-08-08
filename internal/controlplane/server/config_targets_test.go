package server

import (
	"reflect"
	"testing"
)

func TestResolveEffectiveConfig(t *testing.T) {
	group := map[string]any{
		"censorship": map[string]any{"tls_domain": "group.example", "fake_cert_len": float64(100)},
		"general":    map[string]any{"log_level": "info"},
	}
	override := map[string]any{
		"censorship": map[string]any{"tls_domain": "node.example"}, // override wins, sibling kept
		"timeouts":   map[string]any{"client_handshake": float64(5)},
	}
	got := resolveEffectiveConfig(group, override)
	want := map[string]any{
		"censorship": map[string]any{"tls_domain": "node.example", "fake_cert_len": float64(100)},
		"general":    map[string]any{"log_level": "info"},
		"timeouts":   map[string]any{"client_handshake": float64(5)},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v\nwant %#v", got, want)
	}
}

func TestResolveEffectiveConfigNilOverride(t *testing.T) {
	group := map[string]any{"general": map[string]any{"log_level": "info"}}
	got := resolveEffectiveConfig(group, nil)
	if !reflect.DeepEqual(got, group) {
		t.Fatalf("nil override should equal group: %#v", got)
	}
}

func TestResolveEffectiveConfigDoesNotMutateInputs(t *testing.T) {
	group := map[string]any{"censorship": map[string]any{"tls_domain": "g"}}
	override := map[string]any{"censorship": map[string]any{"tls_domain": "o"}}
	_ = resolveEffectiveConfig(group, override)
	if group["censorship"].(map[string]any)["tls_domain"] != "g" {
		t.Fatalf("group input was mutated")
	}
	if override["censorship"].(map[string]any)["tls_domain"] != "o" {
		t.Fatalf("override input was mutated")
	}
}

func TestValidateNoProcessOwnedFieldsRejectsDataPath(t *testing.T) {
	sections := map[string]any{
		"general": map[string]any{"data_path": "/var/lib/telemt/data"},
	}
	err := validateNoProcessOwnedFields(sections)
	if err == nil {
		t.Fatal("expected error for general.data_path, got nil")
	}
}

func TestValidateNoProcessOwnedFieldsAcceptsNormalGeneralField(t *testing.T) {
	sections := map[string]any{
		"general": map[string]any{"log_level": "info"},
	}
	if err := validateNoProcessOwnedFields(sections); err != nil {
		t.Fatalf("unexpected error for normal general field: %v", err)
	}
}

func TestValidateNoDottedKeys(t *testing.T) {
	t.Run("отвергает плоский точечный ключ в схемной секции", func(t *testing.T) {
		err := validateNoDottedKeys(map[string]any{
			"general": map[string]any{"links.public_host": "example.com"},
		})
		if err == nil {
			t.Fatal("ожидалась ошибка на плоский ключ general.links.public_host")
		}
	})

	t.Run("пропускает корректную вложенную таблицу", func(t *testing.T) {
		err := validateNoDottedKeys(map[string]any{
			"general": map[string]any{"links": map[string]any{"public_host": "example.com"}},
		})
		if err != nil {
			t.Fatalf("вложенная таблица должна проходить: %v", err)
		}
	})

	t.Run("разрешает точки в ключах map-контейнеров", func(t *testing.T) {
		err := validateNoDottedKeys(map[string]any{
			"dc_overrides": map[string]any{"203": []any{"91.105.192.100:443"}},
			"censorship": map[string]any{
				"exclusive_mask": map[string]any{"hv24s.metrion.icu": "127.0.0.1:8085"},
			},
		})
		if err != nil {
			t.Fatalf("ключи map-контейнеров должны проходить: %v", err)
		}
	})

	t.Run("ловит точечный ключ во вложенной схемной таблице", func(t *testing.T) {
		err := validateNoDottedKeys(map[string]any{
			"censorship": map[string]any{
				"tls_fetch": map[string]any{"strict.route": true},
			},
		})
		if err == nil {
			t.Fatal("ожидалась ошибка на censorship.tls_fetch.strict.route")
		}
	})
}
