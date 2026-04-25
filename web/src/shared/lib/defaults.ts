// Centralised "magic constants" the UI sometimes hard-codes elsewhere.
// Pulling them here means a fork or a private deployment edits one
// file instead of grepping across feature folders to rebrand the
// install-flow.

// GITHUB_REPO is the canonical install-script source. Forks override
// in source. Wired into the curl one-liner the panel renders for
// operators in the AddServer wizard.
export const GITHUB_REPO = "lost-coder/panvex";

// Telemt's default loopback endpoints. The agent ships with these
// hard-coded too; changing one without changing the other guarantees
// a misconfiguration.
export const DEFAULT_TELEMT_URL = "http://127.0.0.1:9091";
export const DEFAULT_TELEMT_METRICS_URL = "http://127.0.0.1:8081";
