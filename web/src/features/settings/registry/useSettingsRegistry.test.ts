import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderHook, act, waitFor } from "@testing-library/react";
import * as React from "react";
import { describe, expect, it, vi } from "vitest";

// Mock the api module before importing the hook.
vi.mock("@/shared/api/api", () => ({
  apiClient: {
    getSettingsSchema: vi.fn(),
    getSettingsValues: vi.fn(),
    getRestartStatus: vi.fn(),
    putSettingsValues: vi.fn(),
    restartPanel: vi.fn(),
  },
}));

import { apiClient } from "@/shared/api/api";
import { useSettingsRegistry } from "./useSettingsRegistry";
import type { SchemaEntry, ValuesEntry } from "./types";

function makeWrapper() {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return ({ children }: { children: React.ReactNode }) =>
    React.createElement(QueryClientProvider, { client: qc }, children);
}

function makeSchema(overrides: Partial<SchemaEntry> = {}): SchemaEntry {
  return {
    name: "auth.timeout",
    class: "operational",
    type: "int",
    desc: "Timeout in seconds",
    ...overrides,
  };
}

function makeValuesEntry(overrides: Partial<ValuesEntry> = {}): ValuesEntry {
  return {
    value: 30,
    source: "db",
    locked: false,
    ...overrides,
  };
}

const MOCK_SCHEMA: SchemaEntry[] = [
  makeSchema({ name: "auth.timeout", type: "int", min: "1", max: "3600" }),
  makeSchema({ name: "auth.mode", type: "enum", values: ["strict", "lenient"] }),
  makeSchema({ name: "jobs.interval", type: "duration" }),
];

const MOCK_VALUES_RESPONSE = {
  bootstrap: {
    "http.port": makeValuesEntry({ value: 8080, source: "env", locked: true }),
  },
  operational: {
    "auth.timeout": makeValuesEntry({ value: 30 }),
    "auth.mode": makeValuesEntry({ value: "strict" }),
    "jobs.interval": makeValuesEntry({ value: "5m" }),
  },
};

describe("useSettingsRegistry", () => {
  it("returns schema and values after data loads", async () => {
    (apiClient.getSettingsSchema as ReturnType<typeof vi.fn>).mockResolvedValueOnce(MOCK_SCHEMA);
    (apiClient.getSettingsValues as ReturnType<typeof vi.fn>).mockResolvedValueOnce(MOCK_VALUES_RESPONSE);
    (apiClient.getRestartStatus as ReturnType<typeof vi.fn>).mockResolvedValueOnce({ pending: false, fields: [] });

    const { result } = renderHook(() => useSettingsRegistry(), { wrapper: makeWrapper() });

    await waitFor(() => expect(result.current.isLoading).toBe(false));

    expect(result.current.schema).toHaveLength(3);
    expect(result.current.values["auth.timeout"]).toBeDefined();
    expect(result.current.values["http.port"]).toBeDefined(); // bootstrap merged in
  });

  it("partitions bootstrapNames correctly", async () => {
    (apiClient.getSettingsSchema as ReturnType<typeof vi.fn>).mockResolvedValueOnce(MOCK_SCHEMA);
    (apiClient.getSettingsValues as ReturnType<typeof vi.fn>).mockResolvedValueOnce(MOCK_VALUES_RESPONSE);
    (apiClient.getRestartStatus as ReturnType<typeof vi.fn>).mockResolvedValueOnce({ pending: false, fields: [] });

    const { result } = renderHook(() => useSettingsRegistry(), { wrapper: makeWrapper() });

    await waitFor(() => expect(result.current.isLoading).toBe(false));

    expect(result.current.bootstrapNames.has("http.port")).toBe(true);
    expect(result.current.bootstrapNames.has("auth.timeout")).toBe(false);
  });

  it("setDraft updates draft and sets isDirty", async () => {
    (apiClient.getSettingsSchema as ReturnType<typeof vi.fn>).mockResolvedValueOnce(MOCK_SCHEMA);
    (apiClient.getSettingsValues as ReturnType<typeof vi.fn>).mockResolvedValueOnce(MOCK_VALUES_RESPONSE);
    (apiClient.getRestartStatus as ReturnType<typeof vi.fn>).mockResolvedValueOnce({ pending: false, fields: [] });

    const { result } = renderHook(() => useSettingsRegistry(), { wrapper: makeWrapper() });

    await waitFor(() => expect(result.current.isLoading).toBe(false));

    expect(result.current.isDirty).toBe(false);

    act(() => {
      result.current.setDraft("auth.timeout", "60");
    });

    expect(result.current.draft["auth.timeout"]).toBe("60");
    expect(result.current.isDirty).toBe(true);
  });

  it("resetDraft clears draft and isDirty becomes false", async () => {
    (apiClient.getSettingsSchema as ReturnType<typeof vi.fn>).mockResolvedValueOnce(MOCK_SCHEMA);
    (apiClient.getSettingsValues as ReturnType<typeof vi.fn>).mockResolvedValueOnce(MOCK_VALUES_RESPONSE);
    (apiClient.getRestartStatus as ReturnType<typeof vi.fn>).mockResolvedValueOnce({ pending: false, fields: [] });

    const { result } = renderHook(() => useSettingsRegistry(), { wrapper: makeWrapper() });

    await waitFor(() => expect(result.current.isLoading).toBe(false));

    act(() => { result.current.setDraft("auth.timeout", "60"); });
    expect(result.current.isDirty).toBe(true);

    act(() => { result.current.resetDraft(); });
    expect(result.current.isDirty).toBe(false);
    expect(result.current.draft).toEqual({});
  });

  it("validates out-of-range int and returns an error", async () => {
    (apiClient.getSettingsSchema as ReturnType<typeof vi.fn>).mockResolvedValueOnce(MOCK_SCHEMA);
    (apiClient.getSettingsValues as ReturnType<typeof vi.fn>).mockResolvedValueOnce(MOCK_VALUES_RESPONSE);
    (apiClient.getRestartStatus as ReturnType<typeof vi.fn>).mockResolvedValueOnce({ pending: false, fields: [] });

    const { result } = renderHook(() => useSettingsRegistry(), { wrapper: makeWrapper() });

    await waitFor(() => expect(result.current.isLoading).toBe(false));

    // Set a value below min (1)
    act(() => { result.current.setDraft("auth.timeout", "0"); });

    expect(result.current.errors["auth.timeout"]).toBe("min 1");
  });

  it("validates non-integer value and returns an error", async () => {
    (apiClient.getSettingsSchema as ReturnType<typeof vi.fn>).mockResolvedValueOnce(MOCK_SCHEMA);
    (apiClient.getSettingsValues as ReturnType<typeof vi.fn>).mockResolvedValueOnce(MOCK_VALUES_RESPONSE);
    (apiClient.getRestartStatus as ReturnType<typeof vi.fn>).mockResolvedValueOnce({ pending: false, fields: [] });

    const { result } = renderHook(() => useSettingsRegistry(), { wrapper: makeWrapper() });

    await waitFor(() => expect(result.current.isLoading).toBe(false));

    act(() => { result.current.setDraft("auth.timeout", "abc"); });

    expect(result.current.errors["auth.timeout"]).toBe("must be an integer");
  });

  it("validates invalid enum value and returns an error", async () => {
    (apiClient.getSettingsSchema as ReturnType<typeof vi.fn>).mockResolvedValueOnce(MOCK_SCHEMA);
    (apiClient.getSettingsValues as ReturnType<typeof vi.fn>).mockResolvedValueOnce(MOCK_VALUES_RESPONSE);
    (apiClient.getRestartStatus as ReturnType<typeof vi.fn>).mockResolvedValueOnce({ pending: false, fields: [] });

    const { result } = renderHook(() => useSettingsRegistry(), { wrapper: makeWrapper() });

    await waitFor(() => expect(result.current.isLoading).toBe(false));

    act(() => { result.current.setDraft("auth.mode", "unknown"); });

    expect(result.current.errors["auth.mode"]).toMatch(/must be one of/);
  });

  it("save() calls putSettingsValues with coerced draft entries", async () => {
    (apiClient.getSettingsSchema as ReturnType<typeof vi.fn>).mockResolvedValueOnce(MOCK_SCHEMA);
    (apiClient.getSettingsValues as ReturnType<typeof vi.fn>).mockResolvedValue(MOCK_VALUES_RESPONSE);
    (apiClient.getRestartStatus as ReturnType<typeof vi.fn>).mockResolvedValue({ pending: false, fields: [] });
    (apiClient.putSettingsValues as ReturnType<typeof vi.fn>).mockResolvedValueOnce(undefined);

    const { result } = renderHook(() => useSettingsRegistry(), { wrapper: makeWrapper() });

    await waitFor(() => expect(result.current.isLoading).toBe(false));

    act(() => { result.current.setDraft("auth.timeout", "120"); });

    await act(async () => { await result.current.save(); });

    expect(apiClient.putSettingsValues).toHaveBeenCalledWith({ "auth.timeout": 120 });
  });

  it("save() clears draft on success", async () => {
    (apiClient.getSettingsSchema as ReturnType<typeof vi.fn>).mockResolvedValueOnce(MOCK_SCHEMA);
    (apiClient.getSettingsValues as ReturnType<typeof vi.fn>).mockResolvedValue(MOCK_VALUES_RESPONSE);
    (apiClient.getRestartStatus as ReturnType<typeof vi.fn>).mockResolvedValue({ pending: false, fields: [] });
    (apiClient.putSettingsValues as ReturnType<typeof vi.fn>).mockResolvedValueOnce(undefined);

    const { result } = renderHook(() => useSettingsRegistry(), { wrapper: makeWrapper() });

    await waitFor(() => expect(result.current.isLoading).toBe(false));

    act(() => { result.current.setDraft("auth.timeout", "120"); });
    expect(result.current.isDirty).toBe(true);

    await act(async () => { await result.current.save(); });

    expect(result.current.isDirty).toBe(false);
  });

  it("pendingRestart comes from restart-status fields", async () => {
    (apiClient.getSettingsSchema as ReturnType<typeof vi.fn>).mockResolvedValueOnce(MOCK_SCHEMA);
    (apiClient.getSettingsValues as ReturnType<typeof vi.fn>).mockResolvedValueOnce(MOCK_VALUES_RESPONSE);
    (apiClient.getRestartStatus as ReturnType<typeof vi.fn>).mockResolvedValueOnce({
      pending: true,
      fields: ["http.port", "auth.timeout"],
    });

    const { result } = renderHook(() => useSettingsRegistry(), { wrapper: makeWrapper() });

    await waitFor(() => expect(result.current.isLoading).toBe(false));

    expect(result.current.pendingRestart).toEqual(["http.port", "auth.timeout"]);
  });
});
