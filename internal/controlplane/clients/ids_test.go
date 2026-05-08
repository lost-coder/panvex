package clients

import (
	"testing"
)

func TestClientID_String(t *testing.T) {
	id := ClientID("c-123")
	if id.String() != "c-123" {
		t.Fatalf("ClientID.String() = %q, want c-123", id.String())
	}
}

func TestClientID_IsZero(t *testing.T) {
	var zero ClientID
	if !zero.IsZero() {
		t.Fatal("zero ClientID should report IsZero()=true")
	}
	if (ClientID("c-1")).IsZero() {
		t.Fatal("non-zero ClientID should report IsZero()=false")
	}
}
