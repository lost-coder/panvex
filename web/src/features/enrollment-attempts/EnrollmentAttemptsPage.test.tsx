import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";

import "@/shared/lib/i18n";
import { initI18n } from "@/shared/lib/i18n";

// Mock the API aggregator before importing the page. The page only
// hits `listEnrollmentAttempts` (infinite query) and the lazily-
// enabled `getEnrollmentAttempt`; stubbing the first to an empty page
// exercises the empty-state branch without any network.
vi.mock("@/shared/api/api", () => ({
  apiClient: {
    listEnrollmentAttempts: vi.fn(async () => ({ items: [], next_cursor: null })),
    getEnrollmentAttempt: vi.fn(),
  },
}));

import { EnrollmentAttemptsPage } from "./EnrollmentAttemptsPage";

function wrap(node: ReactNode) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return <QueryClientProvider client={qc}>{node}</QueryClientProvider>;
}

describe("EnrollmentAttemptsPage", () => {
  it("renders the title and the empty state when the list is empty", async () => {
    initI18n();
    render(wrap(<EnrollmentAttemptsPage />));
    expect(await screen.findByText(/Enrollment attempts/i)).toBeInTheDocument();
    expect(await screen.findByText(/No attempts match/i)).toBeInTheDocument();
  });
});
