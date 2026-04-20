import type { Meta, StoryObj } from "@storybook/react";
import { ConnectionBadge } from "./ConnectionBadge";

const meta: Meta<typeof ConnectionBadge> = {
  title: "Primitives/ConnectionBadge",
  component: ConnectionBadge,
};
export default meta;

type Story = StoryObj<typeof ConnectionBadge>;

export const Online: Story = { args: { online: true, count: 3 } };
export const Offline: Story = { args: { online: false } };
export const OnlineNoCount: Story = { args: { online: true } };

export const Variants: Story = {
  render: () => (
    <div className="flex items-center gap-3">
      <ConnectionBadge online={true} count={5} />
      <ConnectionBadge online={true} />
      <ConnectionBadge online={false} />
    </div>
  ),
};
