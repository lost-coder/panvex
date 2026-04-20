import type { Meta, StoryObj } from "@storybook/react";
import { MeterRing } from "./MeterRing";

const meta: Meta<typeof MeterRing> = { title: "Primitives/MeterRing", component: MeterRing };
export default meta;
type Story = StoryObj<typeof MeterRing>;

export const Low: Story = { args: { value: 35, label: "Health" } };
export const Medium: Story = { args: { value: 72, label: "Load" } };
export const High: Story = { args: { value: 95, label: "CPU" } };

export const AllStates: Story = {
  render: () => (
    <div className="flex gap-6">
      <div className="relative inline-flex flex-col items-center">
        <MeterRing value={35} label="Health" />
      </div>
      <div className="relative inline-flex flex-col items-center">
        <MeterRing value={72} label="Load" />
      </div>
      <div className="relative inline-flex flex-col items-center">
        <MeterRing value={95} label="CPU" />
      </div>
    </div>
  ),
};
