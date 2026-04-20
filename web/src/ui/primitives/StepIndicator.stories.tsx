// src/primitives/StepIndicator.stories.tsx
import type { Meta, StoryObj } from "@storybook/react";
import { StepIndicator } from "./StepIndicator";

const meta: Meta<typeof StepIndicator> = {
  title: "Primitives/StepIndicator",
  component: StepIndicator,
};
export default meta;
type Story = StoryObj<typeof StepIndicator>;

const steps = ["Configure", "Install", "Connect"];

export const Step1: Story = { args: { steps, current: 0 } };
export const Step2: Story = { args: { steps, current: 1 } };
export const Step3: Story = { args: { steps, current: 2 } };
export const Complete: Story = { args: { steps, current: 3 } };
