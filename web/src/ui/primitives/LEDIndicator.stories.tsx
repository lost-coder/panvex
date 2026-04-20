import type { Meta, StoryObj } from "@storybook/react";
import { LEDIndicator } from "./LEDIndicator";

const meta: Meta<typeof LEDIndicator> = {
  title: "Primitives/LEDIndicator",
  component: LEDIndicator,
  argTypes: {
    status: { control: "select", options: ["ok", "warn", "error"] },
  },
};
export default meta;

type Story = StoryObj<typeof LEDIndicator>;

export const Ok: Story = { args: { status: "ok", label: "FRA", sublabel: "Frankfurt" } };
export const Warn: Story = { args: { status: "warn", label: "SIN", sublabel: "Singapore" } };
export const Error: Story = { args: { status: "error", label: "SYD", sublabel: "Sydney" } };
export const Active: Story = {
  args: { status: "ok", label: "AMS", sublabel: "Amsterdam", active: true },
};

export const Grid: Story = {
  render: () => (
    <div className="grid grid-cols-4 gap-1">
      <LEDIndicator status="ok" label="FRA" sublabel="Frankfurt" />
      <LEDIndicator status="ok" label="AMS" sublabel="Amsterdam" />
      <LEDIndicator status="warn" label="SIN" sublabel="Singapore" />
      <LEDIndicator status="error" label="SYD" sublabel="Sydney" />
      <LEDIndicator status="ok" label="NYC" sublabel="New York" />
      <LEDIndicator status="ok" label="LAX" sublabel="Los Angeles" />
      <LEDIndicator status="ok" label="TKY" sublabel="Tokyo" />
      <LEDIndicator status="ok" label="LDN" sublabel="London" />
    </div>
  ),
};
