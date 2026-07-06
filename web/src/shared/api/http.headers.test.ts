// 7.5 (#web-10): порядок спреда в fetch-инициализаторе. До фикса
// `...init` шёл ПОСЛЕ headers — вызов с init.headers целиком заменял
// объект headers и молча терял Content-Type и X-CSRF-Token.
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { __seedCSRFTokenForTesting, api, apiBasePath } from "./http";

function okJSON(): Response {
  return new Response(JSON.stringify({}), {
    status: 200,
    headers: { "Content-Type": "application/json" },
  });
}

describe("api() init merging", () => {
  const fetchSpy = vi.fn();
  let originalFetch: typeof globalThis.fetch;

  beforeEach(() => {
    originalFetch = globalThis.fetch;
    fetchSpy.mockReset().mockResolvedValue(okJSON());
    globalThis.fetch = fetchSpy as unknown as typeof globalThis.fetch;
    __seedCSRFTokenForTesting("csrf-token-1");
  });

  afterEach(() => {
    globalThis.fetch = originalFetch;
    __seedCSRFTokenForTesting(null);
  });

  it("вызов с init.headers сохраняет Content-Type и X-CSRF-Token", async () => {
    await api(`${apiBasePath}/clients`, {
      method: "POST",
      body: "{}",
      headers: { "X-Custom": "1" },
    });

    expect(fetchSpy).toHaveBeenCalledTimes(1);
    const init = fetchSpy.mock.calls[0]![1] as RequestInit;
    const headers = init.headers as Record<string, string>;
    expect(headers["Content-Type"]).toBe("application/json");
    expect(headers["X-CSRF-Token"]).toBe("csrf-token-1");
    expect(headers["X-Custom"]).toBe("1");
  });

  it("пробрасывает AbortSignal из init в fetch", async () => {
    const controller = new AbortController();
    await api(`${apiBasePath}/clients`, { signal: controller.signal });

    const init = fetchSpy.mock.calls[0]![1] as RequestInit;
    expect(init.signal).toBe(controller.signal);
    expect(init.credentials).toBe("include");
  });
});
