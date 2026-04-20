import type { Meta, StoryObj } from "@storybook/react";
import { TimelineEvent } from "./TimelineEvent";

const meta: Meta<typeof TimelineEvent> = {
  title: "Components/TimelineEvent",
  component: TimelineEvent,
  argTypes: {
    status: { control: "select", options: ["ok", "warn", "error"] },
  },
};
export default meta;

type Story = StoryObj<typeof TimelineEvent>;

export const Ok: Story = {
  args: {
    status: "ok",
    time: "12:04",
    message: "Node restarted successfully",
    detail: "Uptime counter reset. All connections re-established.",
  },
};

export const Error: Story = {
  args: {
    status: "error",
    time: "12:02",
    message: "Connection to SYD lost",
  },
};

export const Timeline: Story = {
  render: () => (
    <div className="w-[340px]">
      <TimelineEvent
        status="ok"
        time="12:04"
        message="Node restarted successfully"
        detail="All 12 DC connections re-established"
      />
      <TimelineEvent status="error" time="12:02" message="Connection to SYD lost" />
      <TimelineEvent
        status="warn"
        time="11:58"
        message="CPU spike detected — 94%"
        detail="Duration: 12s. Triggered autoscale check."
      />
      <TimelineEvent status="ok" time="11:45" message="Config v2.4.1 deployed" />
    </div>
  ),
};
