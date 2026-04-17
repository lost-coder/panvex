import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { api, ApiError, SESSION_EXPIRED_EVENT } from "./api";

// P2-TEST-01 — direct exercise of the `api()` wrapper in api.ts. The
// public apiClient.* helpers all route through this function, so
// coverage here exercises the shared success/error/401 paths that
// every container and hook depends on.
describe("api() wrapper", () => {
  const originalFetch = globalThis.fetch;

  beforeEach(() => {
    // A fresh stub per test so assertions on call args are isolated.
    globalThis.fetch = vi.fn();
  });

  afterEach(() => {
    globalThis.fetch = originalFetch;
    vi.restoreAllMocks();
  });

  it("returns parsed JSON for a 200 response", async () => {
    (globalThis.fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce(
      new Response(JSON.stringify({ ok: true, count: 3 }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      }),
    );

    const data = await api<{ ok: boolean; count: number }>("/api/things");
    expect(data).toEqual({ ok: true, count: 3 });

    const call = (globalThis.fetch as ReturnType<typeof vi.fn>).mock.calls[0];
    expect(call[0]).toBe("/api/things");
    expect(call[1]).toMatchObject({
      credentials: "include",
      headers: expect.objectContaining({ "Content-Type": "application/json" }),
    });
  });

  it("resolves to undefined on 204 No Content (no JSON parse)", async () => {
    (globalThis.fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce(
      new Response(null, { status: 204 }),
    );

    const result = await api<void>("/api/noop", { method: "POST" });
    expect(result).toBeUndefined();
  });

  it("dispatches SESSION_EXPIRED_EVENT on 401 for non-auth paths", async () => {
    const listener = vi.fn();
    window.addEventListener(SESSION_EXPIRED_EVENT, listener);

    (globalThis.fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce(
      new Response(JSON.stringify({ error: "unauthorized", code: "EAUTH" }), {
        status: 401,
      }),
    );

    await expect(api("/api/clients")).rejects.toBeInstanceOf(ApiError);
    expect(listener).toHaveBeenCalledTimes(1);

    window.removeEventListener(SESSION_EXPIRED_EVENT, listener);
  });

  it("does NOT dispatch session-expired for /api/auth/login", async () => {
    const listener = vi.fn();
    window.addEventListener(SESSION_EXPIRED_EVENT, listener);

    (globalThis.fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce(
      new Response(JSON.stringify({ error: "bad creds" }), { status: 401 }),
    );

    await expect(api("/api/auth/login", { method: "POST" })).rejects.toBeInstanceOf(
      ApiError,
    );
    expect(listener).not.toHaveBeenCalled();

    window.removeEventListener(SESSION_EXPIRED_EVENT, listener);
  });

  it("does NOT dispatch session-expired for /api/auth/me bootstrap", async () => {
    const listener = vi.fn();
    window.addEventListener(SESSION_EXPIRED_EVENT, listener);

    (globalThis.fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce(
      new Response(JSON.stringify({ error: "unauthenticated" }), { status: 401 }),
    );

    await expect(api("/api/auth/me")).rejects.toBeInstanceOf(ApiError);
    expect(listener).not.toHaveBeenCalled();

    window.removeEventListener(SESSION_EXPIRED_EVENT, listener);
  });

  it("surfaces server error message + code in ApiError", async () => {
    (globalThis.fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce(
      new Response(
        JSON.stringify({ error: "quota exceeded", code: "E_QUOTA" }),
        { status: 409 },
      ),
    );

    let caught: ApiError | null = null;
    try {
      await api("/api/clients", { method: "POST", body: "{}" });
    } catch (err) {
      caught = err as ApiError;
    }

    expect(caught).toBeInstanceOf(ApiError);
    expect(caught?.message).toBe("quota exceeded");
    expect(caught?.code).toBe("E_QUOTA");
  });

  it("falls back to generic message when error body is not JSON", async () => {
    (globalThis.fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce(
      new Response("not-json", { status: 500 }),
    );

    let caught: ApiError | null = null;
    try {
      await api("/api/boom");
    } catch (err) {
      caught = err as ApiError;
    }

    expect(caught).toBeInstanceOf(ApiError);
    expect(caught?.message).toMatch(/status 500/);
    expect(caught?.code).toBeUndefined();
  });
});
