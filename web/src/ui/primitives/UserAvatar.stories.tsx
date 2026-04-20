import type { Meta, StoryObj } from "@storybook/react";
import { UserAvatar } from "./UserAvatar";

const meta: Meta<typeof UserAvatar> = {
  title: "Primitives/UserAvatar",
  component: UserAvatar,
  argTypes: { size: { control: "select", options: ["sm", "md", "lg"] } },
};
export default meta;

type Story = StoryObj<typeof UserAvatar>;

export const Default: Story = { args: { name: "John Doe" } };
export const Online: Story = { args: { name: "Alice Smith", online: true } };
export const Offline: Story = { args: { name: "Bob Brown", online: false } };
export const Large: Story = { args: { name: "proxy_user_42", size: "lg", online: true } };

export const Group: Story = {
  render: () => (
    <div className="flex items-center gap-2">
      <UserAvatar name="John Doe" online={true} />
      <UserAvatar name="Alice Smith" online={true} />
      <UserAvatar name="proxy_user" online={false} />
      <UserAvatar name="admin" online={true} />
      <UserAvatar name="test_bot" />
    </div>
  ),
};
