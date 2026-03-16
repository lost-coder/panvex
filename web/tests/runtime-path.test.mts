import test from "node:test";
import assert from "node:assert/strict";

import {
  buildEventsURL,
  getRouterBasepath,
  normalizeRootPath,
  resolveAPIBasePath
} from "../src/lib/runtime-path.ts";

test("normalizeRootPath trims and normalizes prefixes", () => {
  assert.equal(normalizeRootPath(""), "");
  assert.equal(normalizeRootPath("/"), "");
  assert.equal(normalizeRootPath("panvex"), "/panvex");
  assert.equal(normalizeRootPath("/panvex/"), "/panvex");
  assert.equal(normalizeRootPath(" /nested/panvex/ "), "/nested/panvex");
});

test("resolveAPIBasePath prefixes api routes with the configured root path", () => {
  assert.equal(resolveAPIBasePath(""), "/api");
  assert.equal(resolveAPIBasePath("/panvex"), "/panvex/api");
});

test("buildEventsURL uses the configured root path for the realtime stream", () => {
  assert.equal(buildEventsURL("https:", "panel.example.com", ""), "wss://panel.example.com/api/events");
  assert.equal(buildEventsURL("https:", "panel.example.com", "/panvex"), "wss://panel.example.com/panvex/api/events");
  assert.equal(buildEventsURL("http:", "127.0.0.1:8080", "/panvex"), "ws://127.0.0.1:8080/panvex/api/events");
});

test("getRouterBasepath returns slash for root and the configured prefix otherwise", () => {
  assert.equal(getRouterBasepath(""), "/");
  assert.equal(getRouterBasepath("/panvex"), "/panvex");
});
