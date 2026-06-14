package runtime

import "testing"

func TestContentHashGateDeltaGates(t *testing.T) {
	gate := &contentHashGate{}

	h1, send1 := gate.next("ok", "", `{"a":1}`)
	if h1 == "" || !send1 {
		t.Fatalf("first call must send body, got hash=%q send=%v", h1, send1)
	}
	h2, send2 := gate.next("ok", "", `{"a":1}`)
	if h2 != h1 || send2 {
		t.Fatalf("unchanged tuple must gate body, got hash=%q send=%v", h2, send2)
	}
	h3, send3 := gate.next("ok", "", `{"a":2}`)
	if h3 == h1 || !send3 {
		t.Fatalf("changed tuple must re-send body, got hash=%q send=%v", h3, send3)
	}
	hAB, _ := (&contentHashGate{}).next("ab", "c")
	hA, _ := (&contentHashGate{}).next("a", "bc")
	if hAB == hA {
		t.Fatal("length-prefixing must keep field boundaries apart")
	}
}

func TestContentHashGateReset(t *testing.T) {
	gate := &contentHashGate{}
	gate.next("ok")
	gate.reset()
	if _, send := gate.next("ok"); !send {
		t.Fatal("after reset the next call must re-send the body")
	}
}
