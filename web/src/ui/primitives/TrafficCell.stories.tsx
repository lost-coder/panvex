import type { Meta, StoryObj } from "@storybook/react";
import { TrafficCell } from "./TrafficCell";

const meta: Meta<typeof TrafficCell> = {
  title: "Primitives/TrafficCell",
  component: TrafficCell,
};
export default meta;

type Story = StoryObj<typeof TrafficCell>;

export const Bytes: Story = { args: { bytes: 512 } };
export const Kilobytes: Story = { args: { bytes: 245_000, label: "↓" } };
export const Megabytes: Story = { args: { bytes: 148_500_000, label: "↑" } };
export const Gigabytes: Story = { args: { bytes: 2_480_000_000 } };
export const Terabytes: Story = { args: { bytes: 5_200_000_000_000 } };

export const AllScales: Story = {
  render: () => (
    <div className="flex flex-col gap-2">
      <TrafficCell bytes={512} label="total" />
      <TrafficCell bytes={245_000} label="↓ down" />
      <TrafficCell bytes={148_500_000} label="↑ up" />
      <TrafficCell bytes={2_480_000_000} label="today" />
      <TrafficCell bytes={5_200_000_000_000} label="all time" />
    </div>
  ),
};
