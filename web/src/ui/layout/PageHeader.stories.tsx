import type { Meta, StoryObj } from "@storybook/react";
import { PageHeader } from "./PageHeader";
import { Button } from "@/ui/base/button";

const meta: Meta<typeof PageHeader> = {
  title: "Layout/PageHeader",
  component: PageHeader,
};
export default meta;

type Story = StoryObj<typeof PageHeader>;

export const Default: Story = {
  args: { title: "Server Detail", subtitle: "node-eu-fra-01" },
};

export const WithAction: Story = {
  args: {
    title: "Nodes",
    subtitle: "25 total",
    trailing: <Button size="sm">Add Node</Button>,
  },
};
