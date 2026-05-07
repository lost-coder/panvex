// Local UI barrel. The kit lives entirely under `web/src/ui/`; each
// sub-slot (tokens, base, primitives, layout, components, compositions)
// re-exports through this file so consumers always import from `@/ui`.
export * from "./tokens";
export * from "./lib";
export * from "./base";
export * from "./primitives";
export * from "./layout";
export * from "./components";
export * from "./compositions";
// 4e.8: page data-contract types moved out of the UI-kit. Re-exported
// from the root barrel so existing import sites keep working without
// another codemod round.
export type * from "@/shared/api/types-pages/pages";
