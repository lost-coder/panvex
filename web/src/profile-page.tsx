import { useQuery } from "@tanstack/react-query";

import { AppearanceSettingsForm } from "./components/appearance-settings-form";
import { SecuritySettingsPanel } from "./components/security-settings-panel";
import { SettingsSection, SettingsState } from "./components/settings-shared";
import { apiClient } from "./lib/api";

export function ProfilePage() {
  const meQuery = useQuery({
    queryKey: ["me"],
    queryFn: () => apiClient.me()
  });

  if (meQuery.isLoading) {
    return <SettingsState title="Loading profile" description="Refreshing your personal security and appearance preferences." />;
  }

  if (meQuery.isError || !meQuery.data) {
    return <SettingsState title="Profile is unavailable" description="The control-plane could not load the current account profile." />;
  }

  return (
    <div className="space-y-6">
      <section className="app-card rounded-[32px]">
        <p className="text-xs font-semibold uppercase tracking-[0.24em] text-[var(--app-text-tertiary)]">Profile</p>
        <h2 className="mt-2 text-3xl font-semibold tracking-tight text-[var(--app-text-primary)]">Your account, comfort, and sign-in protection</h2>
        <p className="mt-3 max-w-3xl text-sm leading-6 text-[var(--app-text-secondary)]">
          Keep personal interface preferences and account protection together without mixing them into shared panel configuration.
        </p>
      </section>

      <SettingsSection
        eyebrow="Profile"
        title="Appearance"
        description="Pick the theme and density that feel best for your day-to-day operator workflow."
      >
        <AppearanceSettingsForm userID={meQuery.data.id} />
      </SettingsSection>

      <SettingsSection
        eyebrow="Profile"
        title="Security"
        description="Manage two-factor protection for your own local account."
      >
        <SecuritySettingsPanel me={meQuery.data} />
      </SettingsSection>
    </div>
  );
}
