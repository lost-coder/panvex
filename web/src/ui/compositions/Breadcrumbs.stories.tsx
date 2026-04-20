import type { Meta, StoryObj } from "@storybook/react";
import { Breadcrumbs } from "./Breadcrumbs";

const meta: Meta<typeof Breadcrumbs> = {
  title: "Compositions/Breadcrumbs",
  component: Breadcrumbs,
};
export default meta;

type Story = StoryObj<typeof Breadcrumbs>;

export const TwoLevels: Story = {
  args: { items: [{ label: "Nodes" }, { label: "node-eu-fra-01" }] },
};

export const ThreeLevels: Story = {
  args: {
    items: [{ label: "Dashboard" }, { label: "Nodes" }, { label: "node-eu-fra-01" }],
  },
};

export const UserDetail: Story = {
  args: {
    items: [{ label: "Users" }, { label: "proxy_user_42" }],
  },
};
