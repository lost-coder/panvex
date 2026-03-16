import { useEffect, useState } from "react";
import { useQuery } from "@tanstack/react-query";

import { PanelSettingsForm } from "./components/panel-settings-form";
import { SecuritySettingsPanel } from "./components/security-settings-panel";
import { SettingsState } from "./components/settings-shared";
import { UsersSettingsPanel } from "./components/users-settings-panel";
import { apiClient } from "./lib/api";
import { getDefaultSettingsTab } from "./settings-page-state";

type SettingsTabID = "panel" | "security" | "users";

type SettingsTab = {
  id: SettingsTabID;
  label: string;
  description: string;
};

const adminTabs: SettingsTab[] = [
  {
    id: "panel",
    label: "Panel",
    description: "Public endpoints, listeners, TLS, and restart-aware panel configuration."
  },
  {
    id: "security",
    label: "Security",
    description: "Two-factor protection for your own local account."
  },
  {
    id: "users",
    label: "Users",
    description: "Compact local access management for the panel."
  }
];

const securityOnlyTabs: SettingsTab[] = [
  {
    id: "security",
    label: "Security",
    description: "Two-factor protection for your own local account."
  }
];

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

    const tabs = meQuery.data.role === "admin" ? adminTabs : securityOnlyTabs;
    if (activeTab === null || !tabs.some((tab) => tab.id === activeTab)) {
      setActiveTab(getDefaultSettingsTab(meQuery.data.role));
    }
  }, [activeTab, meQuery.data]);

  if (meQuery.isLoading) {
    return <SettingsState title="Loading settings" description="Refreshing panel configuration, sign-in security, and local users." />;
  }

  if (meQuery.isError) {
    return <SettingsState title="Settings unavailable" description="The control-plane could not load the current account." />;
  }

  if (!meQuery.data) {
    return <SettingsState title="Settings unavailable" description="The control-plane did not return the current account." />;
  }

  const me = meQuery.data;
  const tabs = me.role === "admin" ? adminTabs : securityOnlyTabs;
  const currentTab = tabs.find((tab) => tab.id === activeTab) ?? tabs.find((tab) => tab.id === getDefaultSettingsTab(me.role)) ?? tabs[0];

  return (
    <section className="rounded-[32px] border border-white/70 bg-white/85 p-6 shadow-[0_20px_60px_rgba(37,46,68,0.08)]">
      <div>
        <p className="text-xs font-semibold uppercase tracking-[0.24em] text-slate-500">Settings</p>
        <h2 className="mt-2 text-3xl font-semibold tracking-tight text-slate-950">Shape how the panel behaves and who can enter it</h2>
        <p className="mt-3 max-w-3xl text-sm leading-6 text-slate-600">Keep network reachability, sign-in safety, and local operators together in one compact place.</p>
        {tabs.length > 1 ? (
          <div className="mt-6 flex flex-wrap gap-3">
            {tabs.map((tab) => (
              <button
                key={tab.id}
                type="button"
                className={`rounded-2xl px-4 py-3 text-sm font-medium transition ${
                  tab.id === currentTab.id
                    ? "bg-slate-950 text-white shadow-[0_16px_32px_rgba(15,23,42,0.18)]"
                    : "border border-slate-200 bg-slate-50 text-slate-700 hover:border-slate-300 hover:bg-white"
                }`}
                onClick={() => setActiveTab(tab.id)}
              >
                {tab.label}
              </button>
            ))}
          </div>
        ) : null}
      </div>

      <div className="mt-6 border-t border-slate-200 pt-6">
        <div className="space-y-4">
          {currentTab.id === "panel" && me.role === "admin" ? <PanelSettingsForm /> : null}
          {currentTab.id === "security" ? <SecuritySettingsPanel me={me} /> : null}
          {currentTab.id === "users" && me.role === "admin" ? <UsersSettingsPanel me={me} /> : null}
        </div>
      </div>
    </section>
  );
}
