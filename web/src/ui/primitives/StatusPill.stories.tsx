import type { Meta, StoryObj } from "@storybook/react";
import { StatusPill } from "./StatusPill";

const meta: Meta<typeof StatusPill> = {
  title: "Primitives/StatusPill",
  component: StatusPill,
};
export default meta;
type Story = StoryObj<typeof StatusPill>;

export const Down: Story = { args: { tone: "error", glyph: "⛔", label: "DOWN" } };
export const Degraded: Story = { args: { tone: "warn", glyph: "▲", label: "DEGRADED" } };
export const Ok: Story = { args: { tone: "ok", glyph: "✓", label: "OK" } };
export const Pending: Story = { args: { tone: "neutral", glyph: "●", label: "PENDING" } };
