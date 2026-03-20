import { useEffect, useState } from "react";
import { useQuery } from "@tanstack/react-query";

import { PanelSettingsForm } from "./components/panel-settings-form";
import { SettingsState } from "./components/settings-shared";
import { UsersSettingsPanel } from "./components/users-settings-panel";
import { apiClient } from "./lib/api";
import { getDefaultSettingsTab, getSettingsTabs, type SettingsTabID } from "./profile-and-settings-state";

export function SettingsPage() {
  const [activeTab, setActiveTab] = useState<SettingsTabID | null>(null);
  const meQuery = useQuery({
    queryKey: ["me"],
    queryFn: () => apiClient.me()
  });

  useEffect(() => {
    if (!meQuery.data) {
      return;
    }

    const tabs = getSettingsTabs(meQuery.data.role);
    if (activeTab === null || !tabs.some((tab) => tab.id === activeTab)) {
      setActiveTab(getDefaultSettingsTab(meQuery.data.role));
    }
  }, [activeTab, meQuery.data]);

  if (meQuery.isLoading) {
    return <SettingsState title="Loading settings" description="Refreshing shared panel configuration and local users." />;
  }

  if (meQuery.isError) {
    return <SettingsState title="Settings unavailable" description="The control-plane could not load the current account." />;
  }

  if (!meQuery.data) {
    return <SettingsState title="Settings unavailable" description="The control-plane did not return the current account." />;
  }

  const me = meQuery.data;
  const tabs = getSettingsTabs(me.role);
  if (tabs.length === 0) {
    return <SettingsState title="Settings are reserved for administrators" description="Profile now contains your personal appearance and security preferences." />;
  }

  const currentTab = tabs.find((tab) => tab.id === activeTab) ?? tabs[0];

  return (
    <section className="app-card rounded-[32px]">
      <div>
        <p className="text-xs font-semibold uppercase tracking-[0.24em] text-[var(--app-text-tertiary)]">Settings</p>
        <h2 className="mt-2 text-3xl font-semibold tracking-tight text-[var(--app-text-primary)]">Shape how the panel behaves and who can manage it</h2>
        <p className="mt-3 max-w-3xl text-sm leading-6 text-[var(--app-text-secondary)]">Keep shared reachability, runtime visibility, and local operator access together in one compact place.</p>
        {tabs.length > 1 ? (
          <div className="mt-6 flex flex-wrap gap-3">
            {tabs.map((tab) => (
              <button
                key={tab.id}
                type="button"
                className={`rounded-2xl px-4 py-3 text-sm font-medium transition ${tab.id === currentTab.id ? "app-tab-active" : "app-tab-inactive"}`}
                onClick={() => setActiveTab(tab.id)}
              >
                {tab.label}
              </button>
            ))}
          </div>
        ) : null}
      </div>

      <div className="mt-6 border-t border-[var(--app-border)] pt-6">
        <div className="space-y-4">
          {currentTab.id === "panel" && me.role === "admin" ? <PanelSettingsForm /> : null}
          {currentTab.id === "users" && me.role === "admin" ? <UsersSettingsPanel me={me} /> : null}
        </div>
      </div>
    </section>
  );
}
