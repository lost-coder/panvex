// R-Q-26: TypeScript 6 enforces stricter side-effect import checks.
// Declare ambient modules for asset imports we rely on (CSS, SVG…)
// so `import "../styles.css"` and friends don't trip TS2882.

declare module "*.css" {
  const content: string;
  export default content;
}

declare module "*.svg" {
  const src: string;
  export default src;
}

declare module "*.png" {
  const src: string;
  export default src;
}

declare module "*.jpg" {
  const src: string;
  export default src;
}

declare module "*.jpeg" {
  const src: string;
  export default src;
}

declare module "*.webp" {
  const src: string;
  export default src;
}

declare module "*.json" {
  const data: unknown;
  export default data;
}
