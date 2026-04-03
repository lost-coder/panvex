import type { SettingsPageProps } from "@panvex/ui";
import type {
  PanelSettingsResponse,
  AppearanceSettingsResponse,
} from "../api";

export function transformSettings(
  panel: PanelSettingsResponse,
  appearance: AppearanceSettingsResponse
): Pick<SettingsPageProps, "panelSettings" | "appearanceSettings"> {
  return {
    panelSettings: {
      httpPublicUrl: panel.http_public_url,
      grpcPublicEndpoint: panel.grpc_public_endpoint,
    },
    appearanceSettings: {
      theme: appearance.theme,
      density: appearance.density,
      helpMode: appearance.help_mode,
      swipeNavigation: false,
    },
  };
}
