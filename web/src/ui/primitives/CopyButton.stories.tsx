import type { Meta, StoryObj } from "@storybook/react";
import { CopyButton } from "./CopyButton";

const meta: Meta<typeof CopyButton> = {
  title: "Primitives/CopyButton",
  component: CopyButton,
};
export default meta;
type Story = StoryObj<typeof CopyButton>;

export const Default: Story = {
  args: { text: "sk-abc123def456" },
  render: (args) => (
    <div className="flex items-center gap-2 text-sm text-fg">
      <span className="font-mono">sk-abc123def456</span>
      <CopyButton {...args} />
    </div>
  ),
};
