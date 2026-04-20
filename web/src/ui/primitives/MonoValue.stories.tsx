import type { Meta, StoryObj } from "@storybook/react";
import { MonoValue } from "./MonoValue";

const meta: Meta<typeof MonoValue> = {
  title: "Primitives/MonoValue",
  component: MonoValue,
};
export default meta;

export const Default: StoryObj<typeof MonoValue> = {
  args: { children: "149.154.167.51:443" },
};
export const Highlighted: StoryObj<typeof MonoValue> = {
  args: { children: "33.3%", className: "text-status-error" },
};
