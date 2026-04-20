import type { Meta, StoryObj } from "@storybook/react";
import { StatusHero } from "./StatusHero";

const meta: Meta<typeof StatusHero> = {
  title: "Compositions/StatusHero",
  component: StatusHero,
};
export default meta;

type Story = StoryObj<typeof StatusHero>;

export const Healthy: Story = {
  args: { online: 24, degraded: 1, offline: 0 },
};

export const WithIssues: Story = {
  args: { online: 18, degraded: 4, offline: 3 },
};
