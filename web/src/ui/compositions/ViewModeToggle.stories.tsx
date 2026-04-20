import type { Meta, StoryObj } from "@storybook/react";
import { useState } from "react";
import { ViewModeToggle } from "./ViewModeToggle";
import type { ViewMode } from "@/shared/api/types-pages/pages";

const meta: Meta<typeof ViewModeToggle> = {
  title: "Compositions/ViewModeToggle",
  component: ViewModeToggle,
};
export default meta;
export const Default: StoryObj<typeof ViewModeToggle> = {
  render: () => {
    const [mode, setMode] = useState<ViewMode>("cards");
    return (
      <div className="flex items-center gap-3 p-4 bg-bg">
        <ViewModeToggle mode={mode} onChange={setMode} />
        <span className="text-xs text-fg-muted">Current: {mode}</span>
      </div>
    );
  },
};
