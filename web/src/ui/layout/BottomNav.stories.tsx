import type { Meta, StoryObj } from "@storybook/react";
import { BottomNav } from "./BottomNav";

const meta: Meta<typeof BottomNav> = {
  title: "Layout/BottomNav",
  component: BottomNav,
  parameters: { layout: "fullscreen", viewport: { defaultViewport: "mobile" } },
};
export default meta;

type Story = StoryObj<typeof BottomNav>;

const navItems = [
  { id: "server", label: "Server", icon: "◉" },
  { id: "dashboard", label: "Dashboard", icon: "⊞" },
  { id: "nodes", label: "Nodes", icon: "☰" },
  { id: "alerts", label: "Alerts", icon: "⚑" },
  { id: "more", label: "More", icon: "⊕" },
];

export const Default: Story = {
  args: { items: navItems, activeId: "server" },
  decorators: [
    (Story) => (
      <div className="relative h-[100px] w-[375px] md:hidden" style={{ display: "flex" }}>
        <Story />
      </div>
    ),
  ],
};
