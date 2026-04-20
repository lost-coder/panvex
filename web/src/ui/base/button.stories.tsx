import type { Meta, StoryObj } from "@storybook/react";
import { Button } from "./button";

const meta: Meta<typeof Button> = {
  title: "UI/Button",
  component: Button,
  argTypes: {
    variant: {
      control: "select",
      options: ["default", "danger", "ghost", "outline"],
    },
    size: { control: "select", options: ["sm", "default", "lg"] },
  },
};
export default meta;

type Story = StoryObj<typeof Button>;

export const Default: Story = { args: { children: "Restart Node" } };
export const Danger: Story = { args: { children: "Force Stop", variant: "danger" } };
export const Ghost: Story = { args: { children: "View Logs", variant: "ghost" } };
export const Outline: Story = { args: { children: "Configure", variant: "outline" } };
export const Small: Story = { args: { children: "Copy", size: "sm" } };
export const Large: Story = { args: { children: "Deploy Changes", size: "lg" } };

export const AllVariants: Story = {
  render: () => (
    <div className="flex flex-wrap gap-3">
      <Button>Default</Button>
      <Button variant="danger">Danger</Button>
      <Button variant="ghost">Ghost</Button>
      <Button variant="outline">Outline</Button>
      <Button disabled>Disabled</Button>
    </div>
  ),
};
