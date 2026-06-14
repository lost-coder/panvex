import { describe, expect, it } from "vitest";
import { relativeTimeParts } from "./useRelativeTime";

// Pure thresholds — i18n + reactivity live in the hook; this is the part
// that must stay correct and is shared by every relative-time label.
describe("relativeTimeParts", () => {
  const now = 1_000_000;

  it("reports justNow under a minute", () => {
    expect(relativeTimeParts(now, now - 30)).toEqual({ key: "justNow" });
  });

  it("reports whole minutes under an hour", () => {
    expect(relativeTimeParts(now, now - 120)).toEqual({ key: "minutesAgo", count: 2 });
  });

  it("reports whole hours under a day", () => {
    expect(relativeTimeParts(now, now - 7_200)).toEqual({ key: "hoursAgo", count: 2 });
  });

  it("reports whole days under the 30-day cutoff", () => {
    expect(relativeTimeParts(now, now - 2 * 86_400)).toEqual({ key: "daysAgo", count: 2 });
  });

  it("falls back to an absolute date past 30 days", () => {
    expect(relativeTimeParts(now, now - 40 * 86_400)).toEqual({ key: "absolute" });
  });

  it("treats a future timestamp as justNow (no negative ages)", () => {
    expect(relativeTimeParts(now, now + 500)).toEqual({ key: "justNow" });
  });
});
