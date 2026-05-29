package server

import (
	"context"
	"testing"
	"time"
)

func TestEffectiveListenAddressFromStore(t *testing.T) {
	srv := testServerWithSQLite(t, time.Date(2026, time.May, 29, 10, 0, 0, 0, time.UTC))

	// Default when unset: Reload populates the registry default for the
	// operational listen addresses on a fresh DB.
	if got := srv.EffectiveHTTPListenAddress(); got != ":8080" {
		t.Errorf("default HTTP listen = %q, want :8080", got)
	}
	if got := srv.EffectiveGRPCListenAddress(); got != ":8443" {
		t.Errorf("default gRPC listen = %q, want :8443", got)
	}

	if err := srv.settings.Put(context.Background(), map[string]string{"http.listen_address": ":9090"}, "test"); err != nil {
		t.Fatalf("Put http: %v", err)
	}
	if got := srv.EffectiveHTTPListenAddress(); got != ":9090" {
		t.Errorf("after Put HTTP listen = %q, want :9090", got)
	}

	if err := srv.settings.Put(context.Background(), map[string]string{"grpc.listen_address": ":9443"}, "test"); err != nil {
		t.Fatalf("Put grpc: %v", err)
	}
	if got := srv.EffectiveGRPCListenAddress(); got != ":9443" {
		t.Errorf("after Put gRPC listen = %q, want :9443", got)
	}
}
