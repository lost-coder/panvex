import type { Meta, StoryObj } from "@storybook/react";
import { AlertStrip } from "./AlertStrip";

const meta: Meta<typeof AlertStrip> = {
  title: "Compositions/AlertStrip",
  component: AlertStrip,
};
export default meta;

type Story = StoryObj<typeof AlertStrip>;

export const Default: Story = {
  args: {
    alerts: [
      {
        severity: "crit",
        message: "Node health below 50%",
        source: "health-monitor",
        timestamp: "12:04",
      },
      {
        severity: "warn",
        message: "Memory usage exceeded 85%",
        source: "resource-watcher",
        timestamp: "12:01",
      },
      { severity: "info", message: "Config reloaded", source: "config-mgr", timestamp: "11:58" },
    ],
  },
};
