package server

import (
	"context"
	"errors"
	"testing"
)

func TestRollingApplyStopsOnFailure(t *testing.T) {
	order := []string{}
	apply := func(_ context.Context, agentID string) error {
		order = append(order, agentID)
		if agentID == "b" {
			return errors.New("boom")
		}
		return nil
	}
	res := rollingApply(context.Background(), []string{"a", "b", "c"}, apply)
	if res.Applied != 1 || res.Failed != "b" || res.Err == nil {
		t.Fatalf("expected stop after b: %+v", res)
	}
	if len(order) != 2 {
		t.Fatalf("c must not be attempted after b fails: %v", order)
	}
}

func TestRollingApplyAllSucceed(t *testing.T) {
	apply := func(context.Context, string) error { return nil }
	res := rollingApply(context.Background(), []string{"a", "b"}, apply)
	if res.Applied != 2 || res.Err != nil || res.Failed != "" {
		t.Fatalf("expected all applied: %+v", res)
	}
}

func TestRollingApplyCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	res := rollingApply(ctx, []string{"a", "b"}, func(context.Context, string) error { return nil })
	if res.Err == nil {
		t.Fatalf("cancelled ctx must stop the roll")
	}
}
