import type { Meta, StoryObj } from "@storybook/react";
import { SLABanner } from "./SLABanner";

const meta: Meta<typeof SLABanner> = {
  title: "Components/SLABanner",
  component: SLABanner,
};
export default meta;

type Story = StoryObj<typeof SLABanner>;

export const Default: Story = {
  args: {
    value: "99.98%",
    label: "SLA Uptime",
    details: "Last 30 days · 8.6 min downtime",
  },
};

export const Perfect: Story = {
  args: {
    value: "100%",
    label: "SLA Uptime",
    details: "Last 30 days · 0 incidents",
  },
};
