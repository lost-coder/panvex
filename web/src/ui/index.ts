// Phase 4e local UI barrel.
//
// After `@/ui-kit-shim` carried consumers through 4a–4d, this barrel
// takes over as files physically move out of @lost-coder/panvex-ui
// into the slots below. Each sub-slot (tokens, base, primitives,
// layout, components, compositions) grows its own `export * from`
// line as its contents land.
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
