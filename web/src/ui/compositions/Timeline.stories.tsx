import type { Meta, StoryObj } from "@storybook/react";
import { Timeline } from "./Timeline";

const meta: Meta<typeof Timeline> = {
  title: "Compositions/Timeline",
  component: Timeline,
};
export default meta;

type Story = StoryObj<typeof Timeline>;

export const Default: Story = {
  args: {
    events: [
      {
        status: "ok",
        time: "12:04",
        message: "Node restarted successfully",
        detail: "All 12 DC connections re-established",
      },
      { status: "error", time: "12:02", message: "Connection to SYD lost" },
      {
        status: "warn",
        time: "11:58",
        message: "CPU spike detected — 94%",
        detail: "Duration: 12s",
      },
      { status: "ok", time: "11:45", message: "Config v2.4.1 deployed" },
      { status: "ok", time: "11:30", message: "Health check passed" },
    ],
  },
  decorators: [
    (Story) => (
      <div className="w-[375px]">
        <Story />
      </div>
    ),
  ],
};
