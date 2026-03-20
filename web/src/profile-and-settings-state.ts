export type SidebarNavigationItem = {
  to: string;
  label: string;
};

export type SettingsTabID = "panel" | "users";

export type SettingsTab = {
  id: SettingsTabID;
  label: string;
  description: string;
};

export type UserMenuItem =
  | {
      kind: "link";
      label: string;
      to: string;
    }
  | {
      kind: "action";
      label: string;
      action: "logout";
    };

const operationalNavigation: SidebarNavigationItem[] = [
  { to: "/", label: "Dashboard" },
  { to: "/fleet", label: "Fleet" },
  { to: "/jobs", label: "Jobs" },
  { to: "/audit", label: "Audit" },
  { to: "/agents", label: "Agents" },
  { to: "/clients", label: "Clients" }
];

const adminSettingsTabs: SettingsTab[] = [
  {
    id: "panel",
    label: "Panel",
    description: "Public endpoints, listeners, TLS, and restart-aware panel configuration."
  },
  {
    id: "users",
    label: "Users",
    description: "Compact local access management for the panel."
  }
];

export function getSidebarNavigation(role: string | undefined): SidebarNavigationItem[] {
  if (role === "admin") {
    return [...operationalNavigation, { to: "/settings", label: "Settings" }];
  }

  return operationalNavigation;
}

export function getUserMenuItems(): UserMenuItem[] {
  return [
    { kind: "link", label: "Profile", to: "/profile" },
    { kind: "action", label: "Log out", action: "logout" }
  ];
}

export function getSettingsTabs(role: string): SettingsTab[] {
  if (role !== "admin") {
    return [];
  }

  return adminSettingsTabs;
}

export function getDefaultSettingsTab(role: string): SettingsTabID {
  if (role !== "admin") {
    return "panel";
  }

  return adminSettingsTabs[0].id;
}
