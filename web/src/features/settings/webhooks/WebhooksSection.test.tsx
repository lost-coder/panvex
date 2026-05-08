import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import * as React from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("@/shared/api/api", async () => {
  const actual = await vi.importActual<typeof import("@/shared/api/api")>(
    "@/shared/api/api",
  );
  return {
    ...actual,
    apiClient: {
      webhookEndpoints: vi.fn(),
      createWebhookEndpoint: vi.fn(),
      updateWebhookEndpoint: vi.fn(),
      deleteWebhookEndpoint: vi.fn(),
    },
  };
});

const confirmMock = vi.fn();
vi.mock("@/app/providers/ConfirmProvider", () => ({
  useConfirm: () => confirmMock,
}));

import { apiClient } from "@/shared/api/api";
import { WebhooksSection } from "./WebhooksSection";

function Wrapper({ children }: { children: React.ReactNode }) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return React.createElement(QueryClientProvider, { client: qc }, children);
}

const sampleEndpoint = {
  id: "wh-abc123",
  name: "ops-slack",
  url: "https://hooks.example.com/T/A/B",
  event_filter: "audit.*",
  allow_private: false,
  enabled: true,
};

describe("WebhooksSection", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    confirmMock.mockReset();
  });

  it("renders the empty state when no endpoints exist", async () => {
    (apiClient.webhookEndpoints as ReturnType<typeof vi.fn>).mockResolvedValue({
      endpoints: [],
    });

    render(<WebhooksSection />, { wrapper: Wrapper });

    expect(
      await screen.findByText(/No webhook endpoints configured yet/i),
    ).toBeTruthy();
  });

  it("renders existing endpoints with status + url", async () => {
    (apiClient.webhookEndpoints as ReturnType<typeof vi.fn>).mockResolvedValue({
      endpoints: [sampleEndpoint],
    });

    render(<WebhooksSection />, { wrapper: Wrapper });

    expect(await screen.findByText("ops-slack")).toBeTruthy();
    expect(screen.getByText(sampleEndpoint.url)).toBeTruthy();
    expect(screen.getByText("Enabled")).toBeTruthy();
  });

  it("creates a new endpoint via the form sheet", async () => {
    (apiClient.webhookEndpoints as ReturnType<typeof vi.fn>).mockResolvedValue({
      endpoints: [],
    });
    (apiClient.createWebhookEndpoint as ReturnType<typeof vi.fn>).mockResolvedValue(
      sampleEndpoint,
    );

    render(<WebhooksSection />, { wrapper: Wrapper });

    const user = userEvent.setup();
    await user.click(await screen.findByRole("button", { name: /add webhook/i }));

    const nameInput = await screen.findByPlaceholderText("ops-slack");
    await user.type(nameInput, "ops-slack");

    const urlInput = screen.getByPlaceholderText(/hooks\.example\.com/);
    await user.clear(urlInput);
    await user.type(urlInput, "https://hooks.example.com/T/A/B");

    // The secret field is the only password input in the form.
    const secretInput = document.querySelector<HTMLInputElement>(
      'input[type="password"]',
    );
    expect(secretInput).not.toBeNull();
    await user.type(secretInput!, "super-secret");

    // Two buttons read "Add Webhook" — the section toolbar (already
    // clicked) and the sheet's submit. After opening, target the submit
    // button by selecting the second match.
    const addButtons = screen.getAllByRole("button", { name: /^add webhook$/i });
    await user.click(addButtons[addButtons.length - 1]!);

    await waitFor(() => {
      expect(apiClient.createWebhookEndpoint).toHaveBeenCalledTimes(1);
    });

    const [payload] = (
      apiClient.createWebhookEndpoint as ReturnType<typeof vi.fn>
    ).mock.calls[0]!;
    expect(payload).toMatchObject({
      name: "ops-slack",
      url: "https://hooks.example.com/T/A/B",
      secret: "super-secret",
      enabled: true,
      allow_private: false,
    });
  });

  it("only deletes after the confirm dialog resolves true", async () => {
    (apiClient.webhookEndpoints as ReturnType<typeof vi.fn>).mockResolvedValue({
      endpoints: [sampleEndpoint],
    });
    (apiClient.deleteWebhookEndpoint as ReturnType<typeof vi.fn>).mockResolvedValue(
      undefined,
    );
    confirmMock.mockResolvedValue(true);

    render(<WebhooksSection />, { wrapper: Wrapper });

    const user = userEvent.setup();
    const deleteBtn = await screen.findByRole("button", {
      name: /delete ops-slack/i,
    });
    await user.click(deleteBtn);

    await waitFor(() => {
      expect(confirmMock).toHaveBeenCalledTimes(1);
    });
    await waitFor(() => {
      expect(apiClient.deleteWebhookEndpoint).toHaveBeenCalledWith("wh-abc123");
    });
  });

  it("does not delete when the operator cancels the confirm dialog", async () => {
    (apiClient.webhookEndpoints as ReturnType<typeof vi.fn>).mockResolvedValue({
      endpoints: [sampleEndpoint],
    });
    confirmMock.mockResolvedValue(false);

    render(<WebhooksSection />, { wrapper: Wrapper });

    const user = userEvent.setup();
    await user.click(
      await screen.findByRole("button", { name: /delete ops-slack/i }),
    );

    await waitFor(() => {
      expect(confirmMock).toHaveBeenCalledTimes(1);
    });
    expect(apiClient.deleteWebhookEndpoint).not.toHaveBeenCalled();
  });
});
