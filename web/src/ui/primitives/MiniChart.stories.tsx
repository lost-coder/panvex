import type { Meta, StoryObj } from "@storybook/react";
import { MiniChart } from "./MiniChart";

const meta: Meta<typeof MiniChart> = { title: "Primitives/MiniChart", component: MiniChart };
export default meta;
type Story = StoryObj<typeof MiniChart>;

export const Stable: Story = { args: { data: [42, 45, 43, 44, 46, 45, 44, 43, 45, 44] } };
export const Spike: Story = {
  args: { data: [30, 32, 35, 40, 85, 92, 45, 38, 35, 33], color: "#f59e0b" },
};
export const Declining: Story = {
  args: { data: [90, 85, 78, 72, 65, 58, 52, 48, 45, 42], color: "#34d399" },
};

export const InCards: Story = {
  render: () => (
    <div className="flex gap-4">
      {[
        { label: "CPU", data: [42, 45, 43, 44, 46, 45, 44, 43, 45, 44], color: "#60a5fa" },
        { label: "MEM", data: [60, 62, 65, 63, 68, 72, 70, 69, 71, 73], color: "#f59e0b" },
        { label: "RTT", data: [12, 11, 13, 12, 14, 12, 11, 12, 13, 12], color: "#34d399" },
      ].map((item) => (
        <div key={item.label} className="rounded-xs bg-bg-card p-3 flex flex-col gap-1">
          <span className="text-[10px] text-fg-muted uppercase">{item.label}</span>
          <MiniChart data={item.data} color={item.color} />
        </div>
      ))}
    </div>
  ),
};
