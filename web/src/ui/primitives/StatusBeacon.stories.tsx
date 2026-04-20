import type { Meta, StoryObj } from "@storybook/react";
import { StatusBeacon } from "./StatusBeacon";

const meta: Meta<typeof StatusBeacon> = {
  title: "Primitives/StatusBeacon",
  component: StatusBeacon,
  argTypes: {
    status: { control: "select", options: ["ok", "warn", "error"] },
    size: { control: "select", options: ["sm", "md", "lg"] },
  },
};
export default meta;

type Story = StoryObj<typeof StatusBeacon>;

export const Ok: Story = { args: { status: "ok" } };
export const Warn: Story = { args: { status: "warn" } };
export const Error: Story = { args: { status: "error" } };
export const Large: Story = { args: { status: "ok", size: "lg" } };
export const Static: Story = { args: { status: "error", animated: false } };

export const AllSizes: Story = {
  render: () => (
    <div className="flex items-center gap-6">
      <StatusBeacon status="ok" size="sm" />
      <StatusBeacon status="warn" size="md" />
      <StatusBeacon status="error" size="lg" />
    </div>
  ),
};
