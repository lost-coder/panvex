import { useState } from "react";
import type { Meta, StoryObj } from "@storybook/react";
import { Select } from "./select";

const meta: Meta<typeof Select> = {
  title: "UI/Select",
  component: Select,
};
export default meta;

type Story = StoryObj<typeof Select>;

const regions = [
  { value: "eu-west", label: "EU West" },
  { value: "us-east", label: "US East" },
  { value: "ap-south", label: "AP South" },
];

function SelectDemo() {
  const [value, setValue] = useState<string>();
  return (
    <Select
      options={regions}
      value={value}
      onChange={setValue}
      placeholder="Select region…"
      className="w-[200px]"
    />
  );
}

export const Default: Story = { render: () => <SelectDemo /> };

export const Disabled: Story = {
  render: () => (
    <Select options={regions} value="eu-west" onChange={() => {}} disabled className="w-[200px]" />
  ),
};
