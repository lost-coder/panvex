import js from "@eslint/js";
import tseslint from "typescript-eslint";
import reactHooks from "eslint-plugin-react-hooks";
import reactRefresh from "eslint-plugin-react-refresh";

export default tseslint.config(
  js.configs.recommended,
  ...tseslint.configs.recommended,
  {
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
      "@typescript-eslint/no-explicit-any": "warn",
      "@typescript-eslint/ban-ts-comment": "warn",
      "react-hooks/set-state-in-effect": "warn",
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
  // Storybook render functions read like component bodies to the
  // react-hooks/rules-of-hooks rule, but Storybook intentionally lets you
  // call hooks inside a story's render for interactive examples. Disable
  // the rule for `*.stories.tsx` so stories stop tripping it.
  {
    files: ["src/**/*.stories.{ts,tsx}"],
    rules: {
      "react-hooks/rules-of-hooks": "off",
    },
  },
  {
    ignores: ["dist/", "node_modules/"],
  }
);
