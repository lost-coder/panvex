import type { Meta, StoryObj } from "@storybook/react";
import { ActionList } from "./ActionList";

const meta: Meta<typeof ActionList> = {
  title: "Compositions/ActionList",
  component: ActionList,
};
export default meta;

type Story = StoryObj<typeof ActionList>;

export const Default: Story = {
  args: {
    actions: [
      { icon: "⟳", label: "Restart Node", description: "Graceful restart with connection drain" },
      { icon: "⚙", label: "Reconfigure", description: "Apply pending configuration changes" },
      { icon: "📋", label: "View Logs", description: "Open real-time log stream" },
      {
        icon: "⏻",
        label: "Force Stop",
        description: "Immediately terminate",
        variant: "danger" as const,
      },
    ],
  },
  decorators: [
    (Story) => (
      <div className="w-[340px]">
        <Story />
      </div>
    ),
  ],
};
