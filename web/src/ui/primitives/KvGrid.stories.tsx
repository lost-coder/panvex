import type { Meta, StoryObj } from "@storybook/react";
import { KvGrid } from "./KvGrid";

const meta: Meta<typeof KvGrid> = {
  title: "Primitives/KvGrid",
  component: KvGrid,
};
export default meta;

export const Default: StoryObj<typeof KvGrid> = {
  render: () => (
    <div className="p-4 bg-bg-card rounded-xs max-w-sm">
      <KvGrid
        rows={[
          { label: "Version", value: "v3.2.1" },
          { label: "Arch / OS", value: "x86_64 / linux" },
          { label: "Build", value: "release" },
          { label: "Uptime", value: "14d 7h" },
        ]}
      />
    </div>
  ),
};
