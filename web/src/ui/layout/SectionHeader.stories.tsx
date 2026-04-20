import type { Meta, StoryObj } from "@storybook/react";
import { SectionHeader } from "./SectionHeader";

const meta: Meta<typeof SectionHeader> = {
  title: "Layout/SectionHeader",
  component: SectionHeader,
};
export default meta;

type Story = StoryObj<typeof SectionHeader>;

export const Default: Story = {
  args: { title: "Data Centers" },
};

export const WithBadge: Story = {
  args: { title: "Alerts", badge: 3 },
};

export const WithTrailing: Story = {
  args: {
    title: "Data Centers",
    badge: 12,
    trailing: <span className="text-xs text-accent cursor-pointer">View All</span>,
  },
};
