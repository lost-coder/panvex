export function getDefaultSettingsTab(role: string): "panel" | "security" {
  if (role === "admin") {
    return "panel";
  }

  return "security";
}
