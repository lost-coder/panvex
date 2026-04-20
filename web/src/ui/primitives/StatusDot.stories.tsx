import type { Meta, StoryObj } from "@storybook/react";
import { StatusDot } from "./StatusDot";

const meta: Meta<typeof StatusDot> = {
  title: "Primitives/StatusDot",
  component: StatusDot,
  argTypes: {
    status: { control: "select", options: ["ok", "warn", "error"] },
    size: { control: "select", options: ["sm", "md"] },
  },
};
export default meta;

type Story = StoryObj<typeof StatusDot>;

export const Ok: Story = { args: { status: "ok" } };
export const Warn: Story = { args: { status: "warn" } };
export const Error: Story = { args: { status: "error" } };
export const Medium: Story = { args: { status: "ok", size: "md" } };
export const Animated: Story = { args: { status: "ok", animated: true } };

export const AllVariants: Story = {
  render: () => (
    <div className="flex items-center gap-4">
      <StatusDot status="ok" />
      <StatusDot status="warn" />
      <StatusDot status="error" />
      <StatusDot status="ok" size="md" />
      <StatusDot status="error" size="md" animated />
    </div>
  ),
};
