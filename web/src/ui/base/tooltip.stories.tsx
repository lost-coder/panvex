import type { Meta, StoryObj } from "@storybook/react";
import { Tooltip } from "./tooltip";
import { Badge } from "@/ui/primitives/Badge";

const meta: Meta<typeof Tooltip> = { title: "UI/Tooltip", component: Tooltip };
export default meta;
type Story = StoryObj<typeof Tooltip>;

export const Default: Story = {
  render: () => (
    <Tooltip content="Coverage: alive_writers / required_writers × 100">
      <span className="text-sm text-fg cursor-help underline decoration-dotted">coverage_pct</span>
    </Tooltip>
  ),
};

export const OnBadge: Story = {
  render: () => (
    <Tooltip content="Accepting new client connections" side="right">
      <span>
        <Badge variant="ok">accepting</Badge>
      </span>
    </Tooltip>
  ),
};
