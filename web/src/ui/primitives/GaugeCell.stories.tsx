import type { Meta, StoryObj } from "@storybook/react";
import { GaugeCell } from "./GaugeCell";

const meta: Meta<typeof GaugeCell> = {
  title: "Primitives/GaugeCell",
  component: GaugeCell,
};
export default meta;

type Story = StoryObj<typeof GaugeCell>;

export const CPU: Story = { args: { value: "42", unit: "%", label: "CPU" } };
export const Memory: Story = { args: { value: "6.1", unit: "GB", label: "Memory" } };
export const Uptime: Story = { args: { value: "14d 7h", label: "Uptime" } };

export const Strip: Story = {
  render: () => (
    <div className="grid grid-cols-2 gap-2 w-[340px]">
      <GaugeCell value="42" unit="%" label="CPU" />
      <GaugeCell value="6.1" unit="GB" label="Memory" />
      <GaugeCell value="14d 7h" label="Uptime" />
      <GaugeCell value="99.98" unit="%" label="Health" />
    </div>
  ),
};
