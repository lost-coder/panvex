import type { Meta, StoryObj } from "@storybook/react";
import { Sidebar } from "./Sidebar";

const meta: Meta<typeof Sidebar> = {
  title: "Layout/Sidebar",
  component: Sidebar,
  parameters: { layout: "fullscreen" },
};
export default meta;

type Story = StoryObj<typeof Sidebar>;

const navItems = [
  { id: "dashboard", label: "Dashboard", icon: "◉" },
  { id: "server", label: "Server", icon: "⊞" },
  { id: "nodes", label: "Nodes", icon: "☰" },
  { id: "alerts", label: "Alerts", icon: "⚑" },
  { id: "settings", label: "Settings", icon: "⊕" },
];

export const Default: Story = {
  args: {
    items: navItems,
    activeId: "dashboard",
    brand: "OPS",
    footer: "v2.4.1",
  },
  decorators: [
    (Story) => (
      <div className="relative h-[600px] w-[60px]">
        <Story />
      </div>
    ),
  ],
};
