import type { Meta, StoryObj } from "@storybook/react";
import { GaugeStrip } from "./GaugeStrip";

const meta: Meta<typeof GaugeStrip> = {
  title: "Compositions/GaugeStrip",
  component: GaugeStrip,
};
export default meta;

type Story = StoryObj<typeof GaugeStrip>;

export const Default: Story = {
  args: {
    items: [
      { value: "42", unit: "%", label: "CPU" },
      { value: "6.1", unit: "GB", label: "Memory" },
      { value: "14d 7h", label: "Uptime" },
      { value: "99.98", unit: "%", label: "Health" },
    ],
  },
};
