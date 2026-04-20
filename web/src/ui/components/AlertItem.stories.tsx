import type { Meta, StoryObj } from "@storybook/react";
import { AlertItem } from "./AlertItem";

const meta: Meta<typeof AlertItem> = {
  title: "Components/AlertItem",
  component: AlertItem,
  argTypes: {
    severity: { control: "select", options: ["crit", "warn", "info"] },
  },
};
export default meta;

type Story = StoryObj<typeof AlertItem>;

export const Critical: Story = {
  args: {
    severity: "crit",
    message: "Node health below 50% — automatic failover in 30s",
    source: "health-monitor",
    timestamp: "12:04:22",
  },
};

export const Warning: Story = {
  args: {
    severity: "warn",
    message: "Memory usage exceeded 85% threshold",
    source: "resource-watcher",
    timestamp: "12:01:15",
  },
};

export const Info: Story = {
  args: {
    severity: "info",
    message: "Configuration reloaded successfully",
    source: "config-mgr",
    timestamp: "11:58:30",
  },
};

export const Stack: Story = {
  render: () => (
    <div className="flex flex-col gap-2 w-[340px]">
      <AlertItem
        severity="crit"
        message="Node health below 50%"
        source="health-monitor"
        timestamp="12:04:22"
      />
      <AlertItem
        severity="warn"
        message="Memory usage exceeded 85%"
        source="resource-watcher"
        timestamp="12:01:15"
      />
      <AlertItem
        severity="info"
        message="Config reloaded"
        source="config-mgr"
        timestamp="11:58:30"
      />
    </div>
  ),
};
