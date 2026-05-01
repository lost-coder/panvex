import type { Meta, StoryObj } from "@storybook/react";
import { TransportBadge } from "./TransportBadge";

const meta: Meta<typeof TransportBadge> = {
  title: "Features/Servers/TransportBadge",
  component: TransportBadge,
};
export default meta;
type Story = StoryObj<typeof TransportBadge>;

export const ME_OK: Story = { args: { mode: "me", healthy: 4, total: 4, severity: "ok" } };
export const Direct_OK: Story = { args: { mode: "direct", healthy: 3, total: 3, severity: "ok" } };
export const Fallback_Warn: Story = { args: { mode: "fallback", healthy: 3, total: 3, severity: "warn" } };
export const Fallback_Critical: Story = { args: { mode: "fallback", healthy: 1, total: 3, severity: "critical" } };
export const MeDown: Story = { args: { mode: "me_down", healthy: 0, total: 0, severity: "critical" } };
