package settings

import (
	"strings"
	"testing"
	"time"
)

func TestValidate_IntInRange(t *testing.T) {
	f := FieldMeta{Type: TypeInt, Min: "8", Max: "64", Name: "x"}
	if _, err := Validate(f, "10"); err != nil {
		t.Fatalf("expected ok, got %v", err)
	}
	if _, err := Validate(f, "7"); err == nil {
		t.Fatal("expected min error")
	}
	if _, err := Validate(f, "65"); err == nil {
		t.Fatal("expected max error")
	}
	if _, err := Validate(f, "abc"); err == nil {
		t.Fatal("expected parse error")
	}
}

func TestValidate_Duration(t *testing.T) {
	f := FieldMeta{Type: TypeDuration, Min: "5s", Max: "10m", Name: "x"}
	v, err := Validate(f, "30s")
	if err != nil {
		t.Fatalf("got err: %v", err)
	}
	if v.(time.Duration) != 30*time.Second {
		t.Fatalf("got %v", v)
	}
	if _, err := Validate(f, "1s"); err == nil {
		t.Fatal("expected min error")
	}
	if _, err := Validate(f, "1h"); err == nil {
		t.Fatal("expected max error")
	}
}

func TestValidate_String_Length(t *testing.T) {
	f := FieldMeta{Type: TypeString, Min: "3", Max: "5", Name: "x"}
	if _, err := Validate(f, "abcd"); err != nil {
		t.Fatal(err)
	}
	if _, err := Validate(f, "ab"); err == nil {
		t.Fatal("expected min")
	}
	if _, err := Validate(f, "abcdef"); err == nil {
		t.Fatal("expected max")
	}
}

func TestValidate_Bool(t *testing.T) {
	f := FieldMeta{Type: TypeBool, Name: "x"}
	for _, in := range []string{"true", "TRUE", "1", "yes", "false", "no", "0"} {
		if _, err := Validate(f, in); err != nil {
			t.Fatalf("input %q: %v", in, err)
		}
	}
	if _, err := Validate(f, "maybe"); err == nil || !strings.Contains(err.Error(), "bool") {
		t.Fatalf("got err = %v", err)
	}
}

func TestValidate_HostPort(t *testing.T) {
	f := FieldMeta{Type: TypeHostPort, Name: "x"}
	for _, ok := range []string{":8080", "127.0.0.1:8080", "[::1]:8443", "panel.example.com:443"} {
		if _, err := Validate(f, ok); err != nil {
			t.Fatalf("%q: %v", ok, err)
		}
	}
	for _, bad := range []string{"notaport", "127.0.0.1", ":notaport", "panel.example.com"} {
		if _, err := Validate(f, bad); err == nil {
			t.Fatalf("%q: expected error", bad)
		}
	}
}

func TestValidate_URL(t *testing.T) {
	f := FieldMeta{Type: TypeURL, Name: "x"}
	for _, ok := range []string{"http://x.example", "https://panel.example/api"} {
		if _, err := Validate(f, ok); err != nil {
			t.Fatalf("%q: %v", ok, err)
		}
	}
	for _, bad := range []string{"x.example", "ftp://x", "https://"} {
		if _, err := Validate(f, bad); err == nil {
			t.Fatalf("%q: expected error", bad)
		}
	}
}

func TestValidate_Enum(t *testing.T) {
	f := FieldMeta{Type: TypeEnum, Name: "x", Values: []string{"sqlite", "postgres"}}
	if _, err := Validate(f, "sqlite"); err != nil {
		t.Fatal(err)
	}
	if _, err := Validate(f, "mysql"); err == nil {
		t.Fatal("expected enum error")
	}
}

func TestValidate_JSON(t *testing.T) {
	f := FieldMeta{Type: TypeJSON, Name: "x"}
	if _, err := Validate(f, `{"k":1}`); err != nil {
		t.Fatal(err)
	}
	if _, err := Validate(f, `{"k":`); err == nil {
		t.Fatal("expected json error")
	}
}
