export function resolvePanelInstallURL(tokenPanelURL: string, origin: string, rootPath: string) {
  const configuredPanelURL = tokenPanelURL.trim();
  if (configuredPanelURL !== "") {
    return configuredPanelURL;
  }

  const normalizedOrigin = origin.trim().replace(/\/+$/, "");
  if (normalizedOrigin === "") {
    return "https://panel.example.com";
  }

  const normalizedRootPath = normalizeRootPath(rootPath);
  if (normalizedRootPath === "") {
    return normalizedOrigin;
  }

  return `${normalizedOrigin}${normalizedRootPath}`;
}

export function buildInstallCommand(panelURL: string, tokenValue: string, agentVersion: string) {
  const versionFlag = agentVersion.trim() !== "" && agentVersion !== "latest" ? ` --version ${agentVersion.trim()}` : "";

  return [
    "curl -fsSL https://github.com/panvex/panvex/releases/latest/download/install-agent.sh | \\",
    "  sudo sh -s -- \\",
    `    --panel-url ${panelURL} \\`,
    `    --enrollment-token ${tokenValue}${versionFlag}`
  ].join("\n");
}

export function buildManualInstallCommand(panelURL: string, tokenValue: string, agentVersion: string) {
  const releaseSegment = agentVersion.trim() !== "" && agentVersion !== "latest" ? `download/${agentVersion.trim()}` : "latest/download";

  return [
    `curl -fsSL -o panvex-agent.tar.gz https://github.com/panvex/panvex/releases/${releaseSegment}/panvex-agent-linux-<amd64|arm64>.tar.gz`,
    "tar -xzf panvex-agent.tar.gz",
    "sudo install -m 0755 panvex-agent /usr/local/bin/panvex-agent",
    "sudo /usr/local/bin/panvex-agent bootstrap \\",
    `  -panel-url ${panelURL} \\`,
    `  -enrollment-token ${tokenValue} \\`,
    "  -state-file /var/lib/panvex-agent/agent-state.json"
  ].join("\n");
}

function normalizeRootPath(rootPath: string) {
  const trimmed = rootPath.trim();
  if (trimmed === "" || trimmed === "/") {
    return "";
  }
  return trimmed.startsWith("/") ? trimmed : `/${trimmed}`;
}
