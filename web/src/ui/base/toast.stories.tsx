import { useState } from "react";
import type { Meta, StoryObj } from "@storybook/react";
import { Toast } from "./toast";
import { Button } from "./button";

const meta: Meta<typeof Toast> = {
  title: "UI/Toast",
  component: Toast,
  parameters: { layout: "fullscreen" },
};
export default meta;

type Story = StoryObj<typeof Toast>;

function ToastDemo({ variant }: { variant: "success" | "error" | "info" }) {
  const [open, setOpen] = useState(false);
  const msgs = {
    success: "Node restarted successfully",
    error: "Failed to connect to SYD",
    info: "Configuration saved",
  };
  return (
    <div className="h-[200px] flex items-center justify-center">
      <Button onClick={() => setOpen(true)}>Show {variant}</Button>
      <Toast open={open} message={msgs[variant]} variant={variant} onClose={() => setOpen(false)} />
    </div>
  );
}

export const Success: Story = { render: () => <ToastDemo variant="success" /> };
export const Error: Story = { render: () => <ToastDemo variant="error" /> };
export const Info: Story = { render: () => <ToastDemo variant="info" /> };
