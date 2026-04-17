export function normalizeRootPath(value: string | undefined | null): string {
  const trimmed = (value ?? "").trim();
  if (trimmed === "" || trimmed === "/") {
    return "";
  }

  const withLeadingSlash = trimmed.startsWith("/") ? trimmed : `/${trimmed}`;
  return withLeadingSlash.replace(/\/+$/, "");
}

export function resolveConfiguredRootPath(): string {
  if (typeof window === "undefined") {
    return "";
  }

  // Runtime root-path is injected by the Go UI handler as a data attribute on
  // <html> (e.g. <html data-root-path="/panvex">). We avoid inline <script>
  // bootstrapping because our CSP disallows 'unsafe-inline'. Fall back to the
  // legacy window.__PANVEX_ROOT_PATH for development tooling that may still
  // set it externally.
  const root = document.documentElement;
  const fromDataset = root ? root.dataset.rootPath : undefined;
  if (fromDataset !== undefined && fromDataset !== "") {
    return normalizeRootPath(fromDataset);
  }

  return normalizeRootPath(
    (window as Window & { __PANVEX_ROOT_PATH?: string }).__PANVEX_ROOT_PATH,
  );
}

export function resolveAPIBasePath(rootPath: string): string {
  const normalized = normalizeRootPath(rootPath);
  return normalized === "" ? "/api" : `${normalized}/api`;
}

export function buildEventsURL(protocol: string, host: string, rootPath: string): string {
  const socketProtocol = protocol === "https:" ? "wss" : "ws";
  return `${socketProtocol}://${host}${resolveAPIBasePath(rootPath)}/events`;
}

export function getRouterBasepath(rootPath: string): string {
  const normalized = normalizeRootPath(rootPath);
  return normalized === "" ? "/" : normalized;
}
