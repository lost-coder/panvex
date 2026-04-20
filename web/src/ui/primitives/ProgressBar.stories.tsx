import type { Meta, StoryObj } from "@storybook/react";
import { ProgressBar } from "./ProgressBar";

const meta: Meta<typeof ProgressBar> = { title: "Primitives/ProgressBar", component: ProgressBar };
export default meta;
type Story = StoryObj<typeof ProgressBar>;

export const Low: Story = { args: { value: 35, label: "CPU" } };
export const Medium: Story = { args: { value: 72, label: "Memory" } };
export const High: Story = { args: { value: 94, label: "Bandwidth" } };
export const Small: Story = { args: { value: 60, size: "sm" } };

export const AllStates: Story = {
  render: () => (
    <div className="flex flex-col gap-4 w-[250px]">
      <ProgressBar value={35} label="CPU" />
      <ProgressBar value={72} label="Memory" />
      <ProgressBar value={94} label="Bandwidth" />
      <ProgressBar value={100} label="Quota" />
    </div>
  ),
};
