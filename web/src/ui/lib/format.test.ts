import { describe, expect, it } from "vitest";
import { formatBytes } from "./format";

describe("formatBytes", () => {
  it("returns raw bytes below 1 KB", () => {
    expect(formatBytes(512)).toBe("512 B");
  });

  it("uses a KB tier between 1 KB and 1 MB", () => {
    expect(formatBytes(523_904)).toBe("523.9 KB");
  });

  it("uses MB above one million bytes", () => {
    expect(formatBytes(512_000_000)).toBe("512.0 MB");
  });

  it("keeps exactly 1e9 in MB (GB threshold is strict)", () => {
    expect(formatBytes(1_000_000_000)).toBe("1000.0 MB");
  });

  it("uses GB strictly above 1e9", () => {
    expect(formatBytes(2_000_000_000)).toBe("2.0 GB");
  });

  it("renders zero as 0 B", () => {
    expect(formatBytes(0)).toBe("0 B");
  });
});
