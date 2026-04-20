import { useState } from "react";
import type { Meta, StoryObj } from "@storybook/react";
import { Toggle } from "./toggle";

const meta: Meta<typeof Toggle> = {
  title: "UI/Toggle",
  component: Toggle,
  argTypes: { size: { control: "select", options: ["sm", "md"] } },
};
export default meta;

type Story = StoryObj<typeof Toggle>;

function ToggleDemo(props: { size?: "sm" | "md"; disabled?: boolean }) {
  const [checked, setChecked] = useState(false);
  return <Toggle checked={checked} onChange={setChecked} {...props} />;
}

export const Default: Story = { render: () => <ToggleDemo /> };
export const Small: Story = { render: () => <ToggleDemo size="sm" /> };
export const Disabled: Story = {
  render: () => <Toggle checked={false} onChange={() => {}} disabled />,
};

export const AllStates: Story = {
  render: () => (
    <div className="flex items-center gap-4">
      <Toggle checked={false} onChange={() => {}} />
      <Toggle checked={true} onChange={() => {}} />
      <Toggle checked={false} onChange={() => {}} disabled />
      <Toggle checked={true} onChange={() => {}} disabled />
    </div>
  ),
};
