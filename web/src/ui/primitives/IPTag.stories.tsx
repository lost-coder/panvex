import type { Meta, StoryObj } from "@storybook/react";
import { IPTag } from "./IPTag";

const meta: Meta<typeof IPTag> = {
  title: "Primitives/IPTag",
  component: IPTag,
};
export default meta;

type Story = StoryObj<typeof IPTag>;

export const IPv4: Story = { args: { address: "192.168.1.42" } };
export const IPv6: Story = { args: { address: "2001:db8::1" } };

export const List: Story = {
  render: () => (
    <div className="flex flex-wrap gap-1.5">
      <IPTag address="185.76.151.1" />
      <IPTag address="91.108.56.100" />
      <IPTag address="149.154.175.50" />
      <IPTag address="2001:67c:4e8::f" />
    </div>
  ),
};
