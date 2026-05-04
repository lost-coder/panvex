import { render } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import type { AuditListItem } from "@/shared/api/types-pages/activity";
import { AuditList } from "./AuditList";

// P-8 (BP-Audit): with 5000 audit events the previous implementation
// emitted 5000+ DOM rows and froze the panel for hundreds of ms on
// initial render. The virtualized version only mounts the slice
// intersecting the viewport plus an overscan buffer.
//
// We assert two things here:
//  1. Render does not throw or time out for a 5000-event input.
//  2. The DOM contains far fewer than 5000 row nodes — verifying the
//     virtualizer is doing its job rather than a regression that mounts
//     every row.

function makeEvents(n: number): AuditListItem[] {
  // Spread events across ~30 days so groupByDay produces multiple
  // headers; otherwise every event would land in a single group, hiding
  // the header-vs-row item split.
  const dayMs = 86_400_000;
  const now = Date.now();
  const out: AuditListItem[] = [];
  for (let i = 0; i < n; i++) {
    const item: AuditListItem = {
      id: `evt-${i}`,
      action: i % 2 === 0 ? "user.login" : "client.update",
      createdAtUnix: Math.floor((now - (i % 30) * dayMs - i * 1000) / 1000),
      actorId: `actor-${i % 17}`,
      targetId: i % 5 === 0 ? `tgt-${i}` : "",
    };
    if (i % 3 === 0) item.actorLabel = `User ${i % 17}`;
    if (i % 5 === 0) item.targetKind = "client";
    out.push(item);
  }
  return out;
}

describe("AuditList virtualization", () => {
  it("renders empty state when events list is empty", () => {
    const { getByText } = render(<AuditList events={[]} />);
    expect(getByText("Audit trail is empty")).toBeTruthy();
  });

  it("renders 5000 events without timing out and only mounts a viewport slice", () => {
    const events = makeEvents(5000);
    const start = performance.now();
    const { container } = render(<AuditList events={events} />);
    const elapsed = performance.now() - start;

    // Generous wall-clock budget — a non-virtualized render of 5000
    // rows in jsdom is on the order of seconds, while the virtualized
    // version finishes in tens of milliseconds. 2s catches a regression
    // back to non-virtualized rendering without flaking on a slow CI box.
    expect(elapsed).toBeLessThan(2000);

    // The virtualized list mounts only the slice that fits in the
    // (jsdom-default) viewport plus overscan. We don't pin the exact
    // count — overscan, height, and item-mix all influence it — but it
    // MUST be far less than the event count. 500 is a comfortable
    // ceiling; in practice we see ~30-60 nodes mounted.
    const rowItems = container.querySelectorAll('[role="listitem"]');
    expect(rowItems.length).toBeLessThan(500);
  });
});
