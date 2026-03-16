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

  return normalizeRootPath((window as Window & { __PANVEX_ROOT_PATH?: string }).__PANVEX_ROOT_PATH);
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
