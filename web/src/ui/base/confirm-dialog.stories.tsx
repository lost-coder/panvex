import { useState } from "react";
import type { Meta, StoryObj } from "@storybook/react";
import { ConfirmDialog } from "./confirm-dialog";
import { Button } from "./button";

const meta: Meta<typeof ConfirmDialog> = {
  title: "UI/ConfirmDialog",
  component: ConfirmDialog,
};
export default meta;

type Story = StoryObj<typeof ConfirmDialog>;

function Demo({ variant }: { variant?: "default" | "danger" }) {
  const [open, setOpen] = useState(false);
  return (
    <>
      <Button variant={variant === "danger" ? "danger" : "default"} onClick={() => setOpen(true)}>
        Open Dialog
      </Button>
      <ConfirmDialog
        open={open}
        title={variant === "danger" ? "Force Stop Node?" : "Restart Node?"}
        description={
          variant === "danger"
            ? "This will immediately terminate all processes and disconnect all clients."
            : "The node will be gracefully restarted. Active connections will be drained."
        }
        confirmLabel={variant === "danger" ? "Force Stop" : "Restart"}
        variant={variant}
        onConfirm={() => setOpen(false)}
        onCancel={() => setOpen(false)}
      />
    </>
  );
}

export const Default: Story = { render: () => <Demo /> };
export const Danger: Story = { render: () => <Demo variant="danger" /> };
