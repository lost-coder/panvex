import type { Meta, StoryObj } from "@storybook/react";
import { EmptyState } from "./EmptyState";
import { Button } from "@/ui/base/button";

const meta: Meta<typeof EmptyState> = {
  title: "Components/EmptyState",
  component: EmptyState,
};
export default meta;

type Story = StoryObj<typeof EmptyState>;

export const Default: Story = {
  args: {
    title: "No users found",
    description: "Try adjusting your search or filter criteria.",
  },
};

export const WithAction: Story = {
  args: {
    icon: "+",
    title: "No nodes configured",
    description: "Add your first server node to get started.",
    action: <Button size="sm">Add Node</Button>,
  },
};

export const NoAlerts: Story = {
  args: {
    icon: "✓",
    title: "All clear",
    description: "No active alerts at this time.",
  },
};
