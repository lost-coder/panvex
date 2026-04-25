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
      // Legacy node:test-based suites — driven via `node --test`, not
      // vitest. After the Phase 4c/4d moves the appearance-helper test
      // lives under shared/lib/; the router node-test file is still at
      // the top of src/ next to the new app/router.tsx location.
      "src/router.test.ts",
      "src/shared/lib/appearance.test.ts",
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
        "src/shared/api/api.ts",
        "src/shared/lib/runtime-path.ts",
        "src/shared/api/transforms/clients.ts",
        "src/app/providers/AuthProvider.tsx",
        "src/app/providers/ToastProvider.tsx",
        "src/features/clients/hooks/useClientsList.ts",
        "src/features/clients/hooks/useClientMutations.ts",
        "src/shared/hooks/useViewMode.ts",
        "src/features/clients/ClientsContainer.tsx",
        "src/features/servers/ServersContainer.tsx",
        "src/features/dashboard/DashboardContainer.tsx",
      ],
      exclude: [
        "src/**/*.test.{ts,tsx}",
        "src/**/*.d.ts",
        "src/app/main.tsx",
        "src/vite-env.d.ts",
      ],
      // Phase-2 §2.2: bump thresholds from the original 40% baseline.
      // Current numbers across the included set:
      //   statements 80%  branches 69%  functions 69%  lines 80%
      // Pin slightly below those values to flag a real regression
      // while absorbing natural test churn — keeps the gate from
      // firing on every PR that adds a small uncovered branch.
      thresholds: {
        statements: 75,
        branches: 65,
        functions: 65,
        lines: 75,
      },
    },
  },
});
