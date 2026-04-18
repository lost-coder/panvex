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
            { group: ["@/shared/api/*"], message: "ui/ may not depend on shared/api; data flows in via props." },
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
            { group: ["@/app/*"], message: "features/ may not depend on app/; use react-router hooks instead of importing the router instance." },
          ],
        },
      ],
    },
  },
  {
    ignores: ["dist/", "node_modules/"],
  }
);
