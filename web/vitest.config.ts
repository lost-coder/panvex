import path from "path";
import { defineConfig } from "vitest/config";
import react from "@vitejs/plugin-react-swc";

// Vitest config for the web baseline test suite (P2-TEST-01).
//
// - `jsdom` environment gives us DOM + `window`/`document` for React
//   Testing Library + Tanstack Router + ToastProvider keyboard handling.
// - `@/` alias mirrors vite.config.ts so test files can import from the
//   same module paths as production code without a separate rewrite.
// - Coverage thresholds are intentionally modest (40% statements) for
//   the baseline; individual follow-up tasks tighten specific modules.
export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: { "@": path.resolve(__dirname, "./src") },
  },
  test: {
    environment: "jsdom",
    globals: true,
    setupFiles: ["./vitest.setup.ts"],
    css: false,
    // Existing node:test files under tests/ and a few legacy *.test.ts/mts
    // are run via `node --test`; exclude them so vitest only owns the
    // vitest-native suites.
    include: ["src/**/*.test.{ts,tsx}"],
    exclude: [
      "node_modules/**",
      "dist/**",
      "tests/**",
      "src/router.test.ts",
      "src/lib/appearance.test.ts",
    ],
    coverage: {
      provider: "v8",
      reporter: ["text", "html", "json-summary"],
      // Baseline scope (P2-TEST-01): we gate on the modules the baseline
      // suite actually exercises. Growing coverage is a per-task
      // responsibility for the remaining hooks/containers — the
      // baseline's job is to lock in the critical auth/toast/api/hooks
      // paths with a meaningful threshold.
      include: [
        "src/lib/api.ts",
        "src/lib/runtime-path.ts",
        "src/lib/transforms/clients.ts",
        "src/providers/AuthProvider.tsx",
        "src/providers/ToastProvider.tsx",
        "src/hooks/useClientsList.ts",
        "src/hooks/useClientMutations.ts",
        "src/hooks/useViewMode.ts",
        "src/containers/ClientsContainer.tsx",
        "src/containers/ServersContainer.tsx",
        "src/containers/DashboardContainer.tsx",
      ],
      exclude: [
        "src/**/*.test.{ts,tsx}",
        "src/**/*.d.ts",
        "src/main.tsx",
        "src/vite-env.d.ts",
      ],
      thresholds: {
        statements: 40,
        branches: 40,
        functions: 40,
        lines: 40,
      },
    },
  },
});
