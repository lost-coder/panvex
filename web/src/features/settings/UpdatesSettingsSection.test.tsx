// R11b Task 3 — component test for UpdatesSettingsSection's server-driven
// self-update phase. Asserts the three phase behaviours the brief calls out:
//   1. failed  -> the operator-readable server message is rendered.
//   2. active  -> the Update button is disabled and the phase is named.
//   3. completed -> a success line ("Updated to {to}") is shown.
//
// Follows the repo's settings-section test pattern (see
// GeoIPSettingsSection.test.tsx): vi.mock the api + guard deps, render under a
// QueryClient. The api module is mocked with importActual so the real
// isActiveSelfUpdatePhase helper (imported by both the hook and the section)
// stays intact while apiClient.getUpdateSettings is stubbed.

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import * as React from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type {
  SelfUpdateState,
  UpdateSettingsResponse,
} from "@/shared/api/api";

vi.mock("@/shared/api/api", async (importActual) => {
  const actual = await importActual<typeof import("@/shared/api/api")>();
  return {
    ...actual,
    apiClient: {
      getUpdateSettings: vi.fn(),
      putUpdateSettings: vi.fn(),
      checkForUpdates: vi.fn(),
      updatePanel: vi.fn(),
    },
  };
});

// useUnsavedChangesGuard (audit E4) calls useBlocker + useConfirm. Mock both so
// the section renders without a Router or ConfirmProvider.
vi.mock("@tanstack/react-router", () => ({ useBlocker: vi.fn() }));
vi.mock("@/app/providers/ConfirmProvider", () => ({
  useConfirm: () => vi.fn().mockResolvedValue(true),
}));

import { apiClient } from "@/shared/api/api";
import { UpdatesSettingsSection } from "./UpdatesSettingsSection";

function makeResponse(
  selfUpdate: Partial<SelfUpdateState>,
  overrides?: Partial<UpdateSettingsResponse>,
): UpdateSettingsResponse {
  return {
    settings: {
      check_interval_hours: 6,
      auto_update_panel: false,
      auto_update_agents: false,
      github_repo: "lost-coder/panvex",
      github_token: "",
      agent_download_source: "github",
      telemt_versions_to_show: 5,
    },
    state: {
      latest_panel_version: "",
      latest_agent_version: "",
      panel_changelog: "",
      agent_changelog: "",
      last_checked_at: 0,
      last_check_error: "",
      telemt_latest_version: "",
      telemt_release_base_url: "",
      telemt_last_check_error: "",
      telemt_available_versions: [],
    },
    current_version: "1.0.0",
    self_update: {
      phase: "",
      from_version: "",
      to_version: "",
      message: "",
      updated_at: 0,
      ...selfUpdate,
    },
    ...overrides,
  };
}

function Wrapper({ children }: { children: React.ReactNode }) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return React.createElement(QueryClientProvider, { client: qc }, children);
}

describe("UpdatesSettingsSection self-update phase", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("renders the server message when the phase is failed", async () => {
    (apiClient.getUpdateSettings as ReturnType<typeof vi.fn>).mockResolvedValue(
      makeResponse({
        phase: "failed",
        from_version: "1.0.0",
        to_version: "1.1.0",
        message: "binary staged, but restart could not be requested; restart the panel manually to finish the update",
      }),
    );

    render(<UpdatesSettingsSection />, { wrapper: Wrapper });

    expect(await screen.findByText("Update failed")).toBeInTheDocument();
    expect(
      screen.getByText(/restart the panel manually to finish the update/i),
    ).toBeInTheDocument();
  });

  it("disables the Update button and names the phase while active", async () => {
    (apiClient.getUpdateSettings as ReturnType<typeof vi.fn>).mockResolvedValue(
      makeResponse(
        { phase: "downloading", from_version: "1.0.0", to_version: "1.1.0" },
        // A newer panel version is advertised so the Update button renders.
        {
          state: {
            latest_panel_version: "1.1.0",
            latest_agent_version: "",
            panel_changelog: "",
            agent_changelog: "",
            last_checked_at: 0,
            last_check_error: "",
            telemt_latest_version: "",
            telemt_release_base_url: "",
            telemt_last_check_error: "",
            telemt_available_versions: [],
          },
        },
      ),
    );

    render(<UpdatesSettingsSection />, { wrapper: Wrapper });

    // Status line names the phase (localized "downloading").
    expect(await screen.findByText(/downloading/i)).toBeInTheDocument();
    // The Update button is disabled while the self-update is active.
    const button = screen.getByRole("button", { name: /updating/i });
    expect(button).toBeDisabled();
  });

  it("shows a success line when the phase is completed", async () => {
    (apiClient.getUpdateSettings as ReturnType<typeof vi.fn>).mockResolvedValue(
      makeResponse({
        phase: "completed",
        from_version: "1.0.0",
        to_version: "1.1.0",
      }),
    );

    render(<UpdatesSettingsSection />, { wrapper: Wrapper });

    expect(await screen.findByText("Updated to 1.1.0")).toBeInTheDocument();
  });
});

// Telemt Version Selection, Task 3 — the "Show latest N Telemt versions"
// field: renders the persisted value, edits go into the draft, and Save PUTs
// the new value alongside the rest of the settings blob.
describe("UpdatesSettingsSection telemt_versions_to_show field", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("renders the persisted value and PUTs an edited value on save", async () => {
    (apiClient.getUpdateSettings as ReturnType<typeof vi.fn>).mockResolvedValue(
      makeResponse({}),
    );
    (apiClient.putUpdateSettings as ReturnType<typeof vi.fn>).mockResolvedValue(
      undefined,
    );

    render(<UpdatesSettingsSection />, { wrapper: Wrapper });

    const input = await screen.findByDisplayValue("5");
    expect(input).toBeInTheDocument();

    // A single atomic change event (rather than userEvent.clear + type)
    // avoids the intermediate empty-string state, which the handler's
    // `Number(e.target.value) || 5` fallback would otherwise snap back to 5.
    fireEvent.change(input, { target: { value: "10" } });
    expect(input).toHaveValue(10);

    const user = userEvent.setup();
    const save = screen.getByRole("button", { name: /^save$/i });
    await waitFor(() => expect(save).toBeEnabled());
    await user.click(save);

    await waitFor(() => {
      expect(apiClient.putUpdateSettings).toHaveBeenCalledTimes(1);
    });
    const putMock = apiClient.putUpdateSettings as ReturnType<typeof vi.fn>;
    expect(putMock.mock.calls[0][0]).toMatchObject({ telemt_versions_to_show: 10 });
  });
});
