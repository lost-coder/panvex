import { render, screen, type RenderOptions } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactElement, ReactNode } from "react";
import { describe, expect, it, vi } from "vitest";

import type { ClientDetailPageProps } from "@/shared/api/types-pages/pages";
import { ClientDetailPage } from "./ClientDetailPage";

// Phase 3 added a `ResetQuotaHistory` Fold to ClientDetailPage that
// pulls /api/audit via tanstack-react-query. The page must be rendered
// inside a QueryClientProvider for that hook to mount — wrap every
// render here so existing smoke-tests stay green.
function renderWithClient(ui: ReactElement, options?: RenderOptions) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  });
  function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
  }
  return render(ui, { wrapper: Wrapper, ...options });
}

// Q5.U-Q-13: smoke-tests for the ClientDetailPage container. Even a
// minimal "renders without throwing + surfaces basic identity fields"
// pass catches refactor regressions where a sub-card import path
// breaks (the IPHistoryCard / LimitsCard split was the immediate
// motivation).

vi.mock("@/features/clients/ClientFormSheet", () => ({
  ClientFormSheet: () => <div data-testid="client-form-sheet" />,
}));

function makeProps(
  overrides: Partial<ClientDetailPageProps["client"]> = {},
): ClientDetailPageProps {
  return {
    client: {
      id: "client-1",
      name: "alpha",
      enabled: true,
      secret: "deadbeef0123456789abcdef01234567",
      userAdTag: "",
      maxTcpConns: 0,
      maxUniqueIps: 0,
      dataQuotaBytes: 0,
      expirationRfc3339: "",
      fleetGroupIds: [],
      agentIds: [],
      trafficUsedBytes: 0,
      uniqueIpsUsed: 0,
      activeTcpConns: 0,
      deployments: [],
      subscriptionUrl: "",
      ...overrides,
    },
  };
}

describe("ClientDetailPage", () => {
  it("renders the client name in the page", () => {
    renderWithClient(<ClientDetailPage {...makeProps({ name: "client-renders" })} />);
    expect(screen.getAllByText("client-renders").length).toBeGreaterThan(0);
  });

  it("renders the page shell without throwing on empty deployments", () => {
    const { container } = renderWithClient(<ClientDetailPage {...makeProps()} />);
    expect(container.querySelectorAll("section").length).toBeGreaterThan(0);
  });

  it("does not crash when the optional ip-history is omitted", () => {
    expect(() =>
      renderWithClient(<ClientDetailPage {...makeProps()} />),
    ).not.toThrow();
  });

  it("renders the Redeploy action when at least one deployment is not yet succeeded", () => {
    const onRedeploy = vi.fn();
    for (const status of ["failed", "queued"] as const) {
      const { unmount } = renderWithClient(
        <ClientDetailPage
          {...makeProps({
            deployments: [
              {
                agentId: "agent-1",
                desiredOperation: "client.create",
                status,
                lastError: status === "failed" ? "telemt rejected" : "",
                links: { classic: [], secure: [], tls: [] },
                lastAppliedAtUnix: 0,
                quotaUsedBytes: 0,
                quotaLastResetUnix: 0,
                panelLastResetUnix: 0,
                quotaResetDrift: false,
              },
            ],
          })}
          onRedeploy={onRedeploy}
        />,
      );
      expect(screen.getAllByRole("button", { name: /redeploy/i }).length).toBeGreaterThan(0);
      unmount();
    }
  });

  it("hides the Redeploy action when every deployment has succeeded", () => {
    renderWithClient(
      <ClientDetailPage
        {...makeProps({
          deployments: [
            {
              agentId: "agent-1",
              desiredOperation: "client.create",
              status: "succeeded",
              lastError: "",
              links: { classic: [], secure: [], tls: [] },
              lastAppliedAtUnix: 0,
              quotaUsedBytes: 0,
              quotaLastResetUnix: 0,
              panelLastResetUnix: 0,
              quotaResetDrift: false,
            },
          ],
        })}
        onRedeploy={vi.fn()}
      />,
    );
    expect(screen.queryByRole("button", { name: /redeploy/i })).toBeNull();
  });

  it("renders a quiet ✓ badge for a healthy active client", () => {
    renderWithClient(<ClientDetailPage {...makeProps({ enabled: true, expirationRfc3339: "" })} />);
    // ok-tone StateBadge is a quiet lucide check-icon chip (replaced the
    // unicode ✓ glyph), not a labelled pill.
    expect(document.querySelectorAll("svg.lucide-check").length).toBeGreaterThan(0);
    // No loud problem-pill label should appear for a healthy client.
    expect(screen.queryByText(/disabled|expired|over quota|deploy failed/i)).toBeNull();
  });

  it("renders a loud status pill for a problem client", () => {
    renderWithClient(<ClientDetailPage {...makeProps({ enabled: false })} />);
    // disabled → neutral StatusPill carrying the translated/keyed label.
    expect(screen.getAllByText(/disabled/i).length).toBeGreaterThan(0);
    // The quiet ok-glyph must NOT render for a problem state.
    expect(screen.queryByText("✓")).toBeNull();
  });
});
