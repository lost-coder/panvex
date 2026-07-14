package server

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

// The certificate-overlap window (R11 / R-1). Issuance and delivery are not
// atomic: the panel records the new credential, then sends the certificate. If
// that exchange is interrupted — panel restart, stream drop — the agent still
// holds the old certificate. A verifier that only knows the new one refuses it,
// and a listen-mode node has no way to ask for another signature: it sat
// stranded until an operator issued a recovery grant.
//
// These tests pin the acceptance rules from both sides of the connection.

func TestRenewalInterruptedNodeKeepsConnectingWithPreviousCert(t *testing.T) {
	now := time.Date(2026, time.July, 14, 12, 0, 0, 0, time.UTC)
	currentTime := now
	server := &Server{now: func() time.Time { return currentTime }}

	overlapUntil := now.Add(24 * time.Hour)
	pins := storage.AgentCertPins{
		Serial:       "serial-new",
		SPKI:         bytes.Repeat([]byte{0x02}, 32),
		PrevSerial:   "serial-old",
		PrevSPKI:     bytes.Repeat([]byte{0x01}, 32),
		OverlapUntil: &overlapUntil,
	}

	// The node never received the renewal response, so it presents the old one.
	if got := server.classifyPresentedSerial(pins, "serial-old"); got != certSerialPrevious {
		t.Fatalf("presenting the previous serial during the overlap = %v, want certSerialPrevious", got)
	}
	// The new one is of course accepted, and it is what closes the window.
	if got := server.classifyPresentedSerial(pins, "serial-new"); got != certSerialCurrent {
		t.Fatalf("presenting the current serial = %v, want certSerialCurrent", got)
	}
	// Anything else stays refused — the overlap widens the accepted set to two
	// credentials, not to any credential.
	if got := server.classifyPresentedSerial(pins, "serial-harvested"); got != certSerialUnknown {
		t.Fatalf("presenting an unknown serial = %v, want certSerialUnknown", got)
	}
}

func TestOverlapWindowExpires(t *testing.T) {
	now := time.Date(2026, time.July, 14, 12, 0, 0, 0, time.UTC)
	currentTime := now
	server := &Server{now: func() time.Time { return currentTime }}

	overlapUntil := now.Add(time.Hour)
	pins := storage.AgentCertPins{
		Serial:       "serial-new",
		SPKI:         bytes.Repeat([]byte{0x02}, 32),
		PrevSerial:   "serial-old",
		PrevSPKI:     bytes.Repeat([]byte{0x01}, 32),
		OverlapUntil: &overlapUntil,
	}

	currentTime = now.Add(2 * time.Hour)
	if got := server.classifyPresentedSerial(pins, "serial-old"); got != certSerialUnknown {
		t.Fatalf("presenting the previous serial after the window closed = %v, want certSerialUnknown", got)
	}
}

func TestClosedOverlapRefusesThePreviousCert(t *testing.T) {
	now := time.Date(2026, time.July, 14, 12, 0, 0, 0, time.UTC)
	server := &Server{now: func() time.Time { return now }}

	// What CloseAgentCertOverlap leaves behind: current only.
	pins := storage.AgentCertPins{Serial: "serial-new", SPKI: bytes.Repeat([]byte{0x02}, 32)}

	if got := server.classifyPresentedSerial(pins, "serial-old"); got != certSerialUnknown {
		t.Fatalf("presenting the previous serial after the overlap closed = %v, want certSerialUnknown", got)
	}
}

// The dial path (listen-mode nodes) must see the same accepted set as the
// inbound path — that is the path where an interrupted renewal actually
// stranded a node.
func TestAcceptedAgentCertPinsIncludesPreviousDuringOverlap(t *testing.T) {
	now := time.Date(2026, time.July, 14, 12, 0, 0, 0, time.UTC)
	currentTime := now
	overlapUntil := now.Add(time.Hour)

	store := &certPinsStub{pins: storage.AgentCertPins{
		Serial:       "serial-new",
		SPKI:         bytes.Repeat([]byte{0x02}, 32),
		PrevSerial:   "serial-old",
		PrevSPKI:     bytes.Repeat([]byte{0x01}, 32),
		OverlapUntil: &overlapUntil,
	}}
	server := &Server{now: func() time.Time { return currentTime }, store: store}
	reader := certPinReader{server: server}

	accepted, err := reader.AcceptedAgentCertPins(context.Background(), "agent-1")
	if err != nil {
		t.Fatalf("AcceptedAgentCertPins() error = %v", err)
	}
	if len(accepted) != 2 {
		t.Fatalf("len(accepted) = %d, want 2 (current + previous)", len(accepted))
	}

	// Once the window lapses, only the current pin is accepted.
	currentTime = now.Add(2 * time.Hour)
	accepted, err = reader.AcceptedAgentCertPins(context.Background(), "agent-1")
	if err != nil {
		t.Fatalf("AcceptedAgentCertPins() after expiry error = %v", err)
	}
	if len(accepted) != 1 || !bytes.Equal(accepted[0], store.pins.SPKI) {
		t.Fatalf("after the window closed, accepted = %d pins, want only the current one", len(accepted))
	}
}

// certPinsStub is a storage.Store whose only implemented method is the one the
// pin reader calls. Anything else would panic, which is exactly what we want
// from a stub: a test that grows a second dependency should say so loudly.
type certPinsStub struct {
	storage.Store
	pins storage.AgentCertPins
}

func (s *certPinsStub) GetAgentCertPins(context.Context, string) (storage.AgentCertPins, error) {
	return s.pins, nil
}
