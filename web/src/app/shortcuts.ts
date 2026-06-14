// Single source of truth for keyboard shortcuts. ShortcutsOverlay renders
// its help list from these specs; ProtectedShell registers the navigation
// handlers. Registration keeps literal useKeyboardShortcut calls (hooks
// can't run in a loop), so when editing this file also update
// ProtectedShell in router.tsx — the overlay test pins the set.
export interface ShortcutSpec {
  keys: string;
  /** ui-namespace i18n key for the help-overlay description. */
  i18nKey: string;
  /** Router path for navigation shortcuts. */
  to?: string;
}

export const NAV_SHORTCUTS: readonly ShortcutSpec[] = [
  { keys: "g d", i18nKey: "shortcuts.goDashboard", to: "/" },
  { keys: "g s", i18nKey: "shortcuts.goServers", to: "/servers" },
  { keys: "g f", i18nKey: "shortcuts.goFleetGroups", to: "/fleet-groups" },
  { keys: "g c", i18nKey: "shortcuts.goClients", to: "/clients" },
  { keys: "g t", i18nKey: "shortcuts.goSettings", to: "/settings" },
];

export const META_SHORTCUTS: readonly ShortcutSpec[] = [
  { keys: "?", i18nKey: "shortcuts.toggleHelp" },
  { keys: "Esc", i18nKey: "shortcuts.closeDialog" },
];
