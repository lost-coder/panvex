import type { Meta, StoryObj } from "@storybook/react";
import { FieldLabel } from "./FieldLabel";

const meta: Meta<typeof FieldLabel> = {
  title: "Primitives/FieldLabel",
  component: FieldLabel,
};
export default meta;

export const Default: StoryObj<typeof FieldLabel> = {
  args: { children: "Section Title" },
};
export const Small: StoryObj<typeof FieldLabel> = {
  args: { children: "Endpoint Writers", size: "xs" },
};
