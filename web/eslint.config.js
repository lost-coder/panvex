import js from "@eslint/js";
import tseslint from "typescript-eslint";
import reactHooks from "eslint-plugin-react-hooks";
import reactRefresh from "eslint-plugin-react-refresh";
import jsxA11y from "eslint-plugin-jsx-a11y";

export default tseslint.config(
  js.configs.recommended,
  ...tseslint.configs.recommended,
  // jsx-a11y recommended rules — accessibility table stakes for Radix/React 19.
  // Loaded as a flat-config plugin block so the recommended ruleset applies to
  // every JSX/TSX file without re-declaring the plugin elsewhere.
  //
  // The downgraded rules below all fire on existing code that needs a
  // dedicated UX pass (autofocus on dialogs, label/Radix-control wiring,
  // non-interactive divs that double as click targets). They stay as
  // `warn` so CI surfaces them without blocking unrelated work; promote
  // to `error` after a focused a11y sweep.
  {
    files: ["**/*.{js,jsx,ts,tsx}"],
    plugins: {
      "jsx-a11y": jsxA11y,
    },
    rules: {
      ...jsxA11y.configs.recommended.rules,
      "jsx-a11y/no-autofocus": "warn",
      "jsx-a11y/label-has-associated-control": "warn",
      "jsx-a11y/click-events-have-key-events": "warn",
      "jsx-a11y/no-noninteractive-element-interactions": "warn",
    },
  },
  {
    // Typed-lint block: enables rules that need type information (e.g.
    // no-floating-promises). Scoped to project sources so non-typed config
    // files (vite.config.ts handled by tsconfig.node.json, eslint.config.js
    // itself) don't crash the parser.
    //
    // Stories are excluded from tsconfig.json's `include` (they ship as
    // dev-only fixtures), so the typed parser cannot resolve them. Story
    // files get the non-typed plugin block below instead.
    files: ["src/**/*.{ts,tsx}"],
    ignores: ["src/**/*.stories.{ts,tsx}"],
    languageOptions: {
      parserOptions: {
        project: ["./tsconfig.json"],
        tsconfigRootDir: import.meta.dirname,
      },
    },
    plugins: {
      "react-hooks": reactHooks,
      "react-refresh": reactRefresh,
    },
    rules: {
      ...reactHooks.configs.recommended.rules,
      "react-refresh/only-export-components": [
        "warn",
        { allowConstantExport: true },
      ],
      "@typescript-eslint/no-unused-vars": [
        "error",
        { argsIgnorePattern: "^_" },
      ],
      // M-19: any erodes the rest of the type guarantees — flip to error
      // so a stray `as any` blocks the next CI run instead of accruing.
      "@typescript-eslint/no-explicit-any": "error",
      "@typescript-eslint/ban-ts-comment": "warn",
      "react-hooks/set-state-in-effect": "warn",
      // BP — pair with verbatimModuleSyntax (Task 6). Auto-fixable.
      "@typescript-eslint/consistent-type-imports": [
        "error",
        {
          prefer: "type-imports",
          fixStyle: "separate-type-imports",
          // `typeof import("…")` shows up in vitest's
          // `vi.importActual<typeof import("…")>` and in a few prop-type
          // shapes that intentionally avoid a top-level cycle. Keep it
          // legal — the rule's main job is steering top-level imports.
          disallowTypeAnnotations: false,
        },
      ],
      // BP — catch fire-and-forget promises. Real bug surface.
      // Currently `warn`: the React Query mutation/invalidation patterns
      // throughout features/* fire promises intentionally; sweeping them
      // to `void mutate()` / `void invalidate()` is its own follow-up
      // (~47 sites). Promote to `error` after that sweep.
      "@typescript-eslint/no-floating-promises": "warn",
      // BP — exhaustive-deps was 'warn' from recommended; promote to error
      // so missing-dep regressions block CI.
      "react-hooks/exhaustive-deps": "error",
    },
  },
  // Migration-plan layering guard (Phase 0.2).
  //
  // Target layout after Phase 4:
  //   src/ui/         design-system primitives — MUST NOT import features/app/shared/api
  //   src/features/*  domain slices — MUST NOT import from src/app
  //   src/app/        router, providers, shell
  //   src/shared/     cross-feature api client, hooks, lib
  //
  // The rules below are active now so that when we start moving files in
  // Phase 4 a violation fails CI immediately — no drift is possible during
  // the migration. The paths are safe today because those directories do
  // not yet exist.
  {
    files: ["src/ui/**/*.{ts,tsx}"],
    rules: {
      "no-restricted-imports": [
        "error",
        {
          patterns: [
            { group: ["@/features/*"], message: "ui/ may not depend on features/; keep the design system domain-agnostic." },
            { group: ["@/app/*"], message: "ui/ may not depend on app/; pass state via props." },
            // Runtime data must flow via props, but prop *types* defined in
            // `@/shared/api/types-pages/pages` are the public contract for
            // pages and compositions. Allow type-only imports so the UI-kit
            // can reuse those shapes without dragging in the runtime module.
            {
              group: ["@/shared/api/*"],
              message: "ui/ may not depend on shared/api at runtime; use `import type` for prop shapes only.",
              allowTypeImports: true,
            },
          ],
        },
      ],
    },
  },
  {
    files: ["src/features/**/*.{ts,tsx}"],
    rules: {
      "no-restricted-imports": [
        "error",
        {
          patterns: [
            // Only the router singleton and the entry module are off-limits
            // — `@/app/providers/*` contexts (Toast, Confirm, Appearance)
            // are the sanctioned way for features to consume cross-cutting
            // UI state via hooks and must stay importable.
            { group: ["@/app/router*"], message: "features/ must not import the router instance; use react-router hooks." },
            { group: ["@/app/main*"], message: "features/ must not import the entry point." },
          ],
        },
      ],
    },
  },
  // Stories — non-typed lint plus the rules-of-hooks waiver. Stories are
  // excluded from tsconfig's `include`, so they can't participate in typed
  // rules (no-floating-promises). The waiver below covers the Storybook
  // render-function hooks pattern; consistent-type-imports still applies via
  // its auto-fixer because it doesn't need type info.
  {
    files: ["src/**/*.stories.{ts,tsx}"],
    plugins: {
      "react-hooks": reactHooks,
      "react-refresh": reactRefresh,
    },
    rules: {
      ...reactHooks.configs.recommended.rules,
      "react-hooks/rules-of-hooks": "off",
      "@typescript-eslint/consistent-type-imports": [
        "error",
        {
          prefer: "type-imports",
          fixStyle: "separate-type-imports",
          // `typeof import("…")` shows up in vitest's
          // `vi.importActual<typeof import("…")>` and in a few prop-type
          // shapes that intentionally avoid a top-level cycle. Keep it
          // legal — the rule's main job is steering top-level imports.
          disallowTypeAnnotations: false,
        },
      ],
    },
  },
  {
    ignores: ["dist/", "node_modules/"],
  }
);
