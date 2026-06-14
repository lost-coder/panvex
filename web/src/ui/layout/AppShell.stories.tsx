import type { Meta, StoryObj } from "@storybook/react";
import { AppShell } from "./AppShell";
import { PageHeader } from "./PageHeader";
import { SectionHeader } from "./SectionHeader";
import { GaugeStrip } from "@/ui/compositions/GaugeStrip";
import { AlertStrip } from "@/ui/compositions/AlertStrip";

const meta: Meta<typeof AppShell> = {
  title: "Layout/AppShell",
  component: AppShell,
  parameters: { layout: "fullscreen" },
};
export default meta;

type Story = StoryObj<typeof AppShell>;

const navItems = [
  { id: "dashboard", label: "Dashboard", icon: "◉" },
  { id: "server", label: "Server", icon: "⊞" },
  { id: "nodes", label: "Nodes", icon: "☰" },
  { id: "alerts", label: "Alerts", icon: "⚑" },
  { id: "settings", label: "Settings", icon: "⊕" },
];

export const DashboardPage: Story = {
  args: {
    navItems,
    activeId: "dashboard",
    brand: "OPS",
    sidebarFooter: "v2.4.1",
    children: (
      <div className="flex flex-col gap-4">
        <PageHeader title="Dashboard" subtitle="System-wide overview" />
        <div className="px-4 md:px-8 flex flex-col gap-6">
          <div>
            <SectionHeader title="Server Metrics" />
            <GaugeStrip
              items={[
                { value: "42", unit: "%", label: "AVG CPU" },
                { value: "6.1", unit: "GB", label: "AVG MEM" },
                { value: "25", label: "Total Nodes" },
                { value: "99.9", unit: "%", label: "SLA" },
              ]}
            />
          </div>
          <div>
            <SectionHeader title="Alerts" badge={2} />
            <AlertStrip
              alerts={[
                {
                  severity: "crit",
                  message: "Node health below 50%",
                  source: "monitor",
                  timestamp: "12:04",
                },
                {
                  severity: "warn",
                  message: "Memory threshold exceeded",
                  source: "watcher",
                  timestamp: "12:01",
                },
              ]}
            />
          </div>
        </div>
      </div>
    ),
  },
};
