import type { Meta, StoryObj } from "@storybook/react";
import { Badge } from "./Badge";

const meta: Meta<typeof Badge> = {
  title: "Primitives/Badge",
  component: Badge,
  argTypes: {
    variant: { control: "select", options: ["default", "ok", "warn", "error", "accent"] },
  },
};
export default meta;

type Story = StoryObj<typeof Badge>;

export const Default: Story = { args: { children: "eu-west" } };
export const Ok: Story = { args: { children: "online", variant: "ok" } };
export const Warn: Story = { args: { children: "degraded", variant: "warn" } };
export const Error: Story = { args: { children: "offline", variant: "error" } };
export const Accent: Story = { args: { children: "admin", variant: "accent" } };

export const AllVariants: Story = {
  render: () => (
    <div className="flex flex-wrap gap-2">
      <Badge>default</Badge>
      <Badge variant="ok">online</Badge>
      <Badge variant="warn">degraded</Badge>
      <Badge variant="error">offline</Badge>
      <Badge variant="accent">admin</Badge>
    </div>
  ),
};
