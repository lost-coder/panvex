import type { Meta, StoryObj } from "@storybook/react";
import { ActionItem } from "./ActionItem";

const meta: Meta<typeof ActionItem> = {
  title: "Components/ActionItem",
  component: ActionItem,
  argTypes: {
    variant: { control: "select", options: ["default", "danger"] },
  },
};
export default meta;

type Story = StoryObj<typeof ActionItem>;

export const Default: Story = {
  args: { icon: "⟳", label: "Restart Node", description: "Graceful restart with connection drain" },
};

export const Danger: Story = {
  args: {
    icon: "⏻",
    label: "Force Stop",
    description: "Immediately terminate all processes",
    variant: "danger",
  },
};

export const ActionList: Story = {
  render: () => (
    <div className="w-[340px] flex flex-col">
      <ActionItem
        icon="⟳"
        label="Restart Node"
        description="Graceful restart with connection drain"
      />
      <ActionItem icon="⚙" label="Reconfigure" description="Apply pending configuration changes" />
      <ActionItem icon="📋" label="View Logs" description="Open real-time log stream" />
      <ActionItem
        icon="⏻"
        label="Force Stop"
        description="Immediately terminate"
        variant="danger"
      />
    </div>
  ),
};
