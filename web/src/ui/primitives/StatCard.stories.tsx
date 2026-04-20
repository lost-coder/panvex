import type { Meta, StoryObj } from "@storybook/react";
import { StatCard } from "./StatCard";

const meta: Meta<typeof StatCard> = {
  title: "Primitives/StatCard",
  component: StatCard,
};
export default meta;

export const Default: StoryObj<typeof StatCard> = {
  render: () => (
    <div className="grid grid-cols-3 gap-2 p-4 bg-bg">
      <StatCard label="Alive Writers" value="54" />
      <StatCard label="Coverage %" value="95.3%" />
      <StatCard label="Required Writers" value="43" />
    </div>
  ),
};
