import { afterEach, beforeEach, describe, expect, it } from "vitest";
import {
  normalizeRootPath,
  resolveAPIBasePath,
  resolveConfiguredRootPath,
  buildEventsURL,
  getRouterBasepath,
} from "./runtime-path";

describe("normalizeRootPath", () => {
  it("treats empty string, undefined, and '/' as empty", () => {
    expect(normalizeRootPath("")).toBe("");
    expect(normalizeRootPath(undefined)).toBe("");
    expect(normalizeRootPath(null)).toBe("");
    expect(normalizeRootPath("/")).toBe("");
  });

  it("ensures a leading slash and trims trailing slashes", () => {
    expect(normalizeRootPath("panvex")).toBe("/panvex");
    expect(normalizeRootPath("/panvex///")).toBe("/panvex");
  });
});

describe("resolveAPIBasePath", () => {
  it("returns /api when no root path is set", () => {
    expect(resolveAPIBasePath("")).toBe("/api");
  });

  it("prefixes with root path when set", () => {
    expect(resolveAPIBasePath("/panvex")).toBe("/panvex/api");
  });
});

describe("getRouterBasepath", () => {
  it("returns '/' when empty, otherwise the normalized root", () => {
    expect(getRouterBasepath("")).toBe("/");
    expect(getRouterBasepath("/panvex")).toBe("/panvex");
  });
});

describe("buildEventsURL", () => {
  it("maps https -> wss", () => {
    expect(buildEventsURL("https:", "host:8080", "/panvex")).toBe(
      "wss://host:8080/panvex/api/events",
    );
  });

  it("maps http -> ws and handles empty root path", () => {
    expect(buildEventsURL("http:", "host:8080", "")).toBe(
      "ws://host:8080/api/events",
    );
  });
});

describe("resolveConfiguredRootPath", () => {
  const originalDataset = document.documentElement.dataset.rootPath;
  const w = window as Window & { __PANVEX_ROOT_PATH?: string };
  const originalGlobal = w.__PANVEX_ROOT_PATH;

  beforeEach(() => {
    // Reset between tests so one branch doesn't leak into another.
    delete document.documentElement.dataset.rootPath;
    delete w.__PANVEX_ROOT_PATH;
  });

  afterEach(() => {
    if (originalDataset !== undefined) {
      document.documentElement.dataset.rootPath = originalDataset;
    }
    if (originalGlobal !== undefined) {
      w.__PANVEX_ROOT_PATH = originalGlobal;
    }
  });

  it("returns '' when nothing is configured", () => {
    expect(resolveConfiguredRootPath()).toBe("");
  });

  it("reads from the <html data-root-path> attribute", () => {
    document.documentElement.dataset.rootPath = "/panvex";
    expect(resolveConfiguredRootPath()).toBe("/panvex");
  });

  it("falls back to window.__PANVEX_ROOT_PATH", () => {
    w.__PANVEX_ROOT_PATH = "/legacy";
    expect(resolveConfiguredRootPath()).toBe("/legacy");
  });
});
