// src/primitives/InitCard.stories.tsx
import type { Meta, StoryObj } from "@storybook/react";
import { InitCard } from "./InitCard";

const meta: Meta<typeof InitCard> = {
  title: "Primitives/InitCard",
  component: InitCard,
};
export default meta;
type Story = StoryObj<typeof InitCard>;

export const Initializing: Story = {
  args: {
    stage: "connecting_dcs",
    progressPct: 62,
    attempt: 1,
    retryLimit: 3,
    elapsedSecs: 12,
  },
};

export const WithError: Story = {
  args: {
    stage: "connecting_dcs",
    progressPct: 45,
    attempt: 2,
    retryLimit: 3,
    elapsedSecs: 38,
    lastError: "connection refused: dc3.telegram.org:443",
  },
};

export const Degraded: Story = {
  args: {
    stage: "me_pool_init",
    progressPct: 80,
    attempt: 1,
    retryLimit: 3,
    elapsedSecs: 25,
    degraded: true,
  },
};
