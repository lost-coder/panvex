import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import type { ClientDetailPageProps } from "@/shared/api/types-pages/pages";
import { ClientDetailPage } from "./ClientDetailPage";

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
      ...overrides,
    },
  };
}

describe("ClientDetailPage", () => {
  it("renders the client name in the page", () => {
    render(<ClientDetailPage {...makeProps({ name: "client-renders" })} />);
    expect(screen.getAllByText("client-renders").length).toBeGreaterThan(0);
  });

  it("renders the page shell without throwing on empty deployments", () => {
    const { container } = render(<ClientDetailPage {...makeProps()} />);
    expect(container.querySelectorAll("section").length).toBeGreaterThan(0);
  });

  it("does not crash when the optional ip-history is omitted", () => {
    expect(() =>
      render(<ClientDetailPage {...makeProps()} />),
    ).not.toThrow();
  });

  it("renders the Redeploy action when at least one deployment is not yet succeeded", () => {
    const onRedeploy = vi.fn();
    for (const status of ["failed", "queued"] as const) {
      const { unmount } = render(
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
    render(
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
            },
          ],
        })}
        onRedeploy={vi.fn()}
      />,
    );
    expect(screen.queryByRole("button", { name: /redeploy/i })).toBeNull();
  });
});
