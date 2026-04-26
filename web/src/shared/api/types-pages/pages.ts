// Barrel re-export façade for page-level type modules. Each domain
// owns its types in a sibling file; this file keeps the historical
// `@/shared/api/types-pages/pages` import path working without
// forcing every consumer to update. Add new domains by exporting them
// from the matching file alongside this one.

export * from "./common";
export * from "./dashboard";
export * from "./servers";
export * from "./server-detail-data";
export * from "./server-detail";
export * from "./clients";
export * from "./discovered-clients";
export * from "./client-form";
export * from "./enrollment";
export * from "./users";
export * from "./profile";
export * from "./settings";
export * from "./login";
export * from "./activity";
