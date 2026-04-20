// src/primitives/SecretReveal.stories.tsx
import type { Meta, StoryObj } from "@storybook/react";
import { SecretReveal } from "./SecretReveal";

const meta: Meta<typeof SecretReveal> = {
  title: "Primitives/SecretReveal",
  component: SecretReveal,
};
export default meta;
type Story = StoryObj<typeof SecretReveal>;

export const Default: Story = {
  args: {
    secret: "cl_sk_a7f3b2c8d9e0f1234567890abcdef1234567890abcdef",
    onDismiss: () => {},
  },
};
