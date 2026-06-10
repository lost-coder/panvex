// Task 22 — component test for GeoIPSettingsSection.
//
// Asserts the section's core flow:
//   1. Loads the current settings via apiClient.getGeoIPSettings (Disabled).
//   2. User picks the Auto radio.
//   3. Clicks Save -> apiClient.putGeoIPSettings fires with mode "auto" and
//      the success toast is invoked.
//
// The repo's other settings hook tests use vi.mock for the api + toast
// modules (see useClientMutations.test.tsx); we follow that pattern here
// rather than introducing an MSW dependency just for this one case.

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import * as React from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type { GeoIPResponseParsed } from "@/shared/api/schemas";

vi.mock("@/shared/api/api", () => ({
  apiClient: {
    getGeoIPSettings: vi.fn(),
    putGeoIPSettings: vi.fn(),
    refreshGeoIP: vi.fn(),
  },
}));

const toastApi = {
  success: vi.fn(),
  error: vi.fn(),
  info: vi.fn(),
  withAction: vi.fn(),
  dismiss: vi.fn(),
};
vi.mock("@/app/providers/ToastProvider", () => ({
  useToast: () => toastApi,
}));

// useUnsavedChangesGuard (audit E4) calls useBlocker + useConfirm.
// Mock both so the section renders without a Router or ConfirmProvider.
vi.mock("@tanstack/react-router", () => ({ useBlocker: vi.fn() }));
vi.mock("@/app/providers/ConfirmProvider", () => ({
  useConfirm: () => vi.fn().mockResolvedValue(true),
}));

import { apiClient } from "@/shared/api/api";
import { GeoIPSettingsSection } from "./GeoIPSettingsSection";

function emptySource() {
  return {
    last_checked_at: 0,
    last_updated_at: 0,
    etag: "",
    path: "",
    size_bytes: 0,
    error: "",
  };
}

function makeResponse(mode: "" | "auto" | "url" | "local"): GeoIPResponseParsed {
  return {
    settings: {
      mode,
      city: { enabled: true, url: "", local_path: "" },
      asn: { enabled: true, url: "", local_path: "" },
    },
    state: {
      city: emptySource(),
      asn: emptySource(),
    },
  };
}

function Wrapper({ children }: { children: React.ReactNode }) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return React.createElement(QueryClientProvider, { client: qc }, children);
}

describe("GeoIPSettingsSection", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("renders Disabled mode initially, then PUTs an Auto change and shows success toast", async () => {
    const initial = makeResponse("");
    const saved = makeResponse("auto");

    (apiClient.getGeoIPSettings as ReturnType<typeof vi.fn>).mockResolvedValue(
      initial,
    );
    (apiClient.putGeoIPSettings as ReturnType<typeof vi.fn>).mockResolvedValue(
      saved,
    );

    render(<GeoIPSettingsSection />, { wrapper: Wrapper });

    // The component renders "Loading…" until the GET resolves; findByLabelText
    // awaits the post-load DOM where the radios actually exist.
    const disabledRadio = await screen.findByLabelText(/Disabled/i);
    expect(disabledRadio).toBeChecked();

    const autoRadio = screen.getByLabelText(/Auto \(P3TERX\)/i);
    expect(autoRadio).not.toBeChecked();

    // Pick "Auto" — this dirties the form so Save becomes enabled.
    const user = userEvent.setup();
    await user.click(autoRadio);
    expect(autoRadio).toBeChecked();

    const save = screen.getByRole("button", { name: /^save$/i });
    await waitFor(() => expect(save).toBeEnabled());
    await user.click(save);

    // PUT fires exactly once with the new mode; success toast follows.
    await waitFor(() => {
      expect(apiClient.putGeoIPSettings).toHaveBeenCalledTimes(1);
    });
    const [payload] = (
      apiClient.putGeoIPSettings as ReturnType<typeof vi.fn>
    ).mock.calls[0]!;
    expect((payload as GeoIPResponseParsed["settings"]).mode).toBe("auto");

    await waitFor(() => {
      expect(toastApi.success).toHaveBeenCalledWith("GeoIP settings saved.");
    });
  });
});
