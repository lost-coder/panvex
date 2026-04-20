// src/primitives/ChipToggle.stories.tsx
import type { Meta, StoryObj } from "@storybook/react";
import { useState } from "react";
import { ChipToggle } from "./ChipToggle";

const meta: Meta<typeof ChipToggle> = {
  title: "Primitives/ChipToggle",
  component: ChipToggle,
};
export default meta;
type Story = StoryObj<typeof ChipToggle>;

export const Default: Story = {
  render: () => {
    const [selected, setSelected] = useState(false);
    return (
      <ChipToggle
        label="europe"
        sublabel="12 nodes"
        selected={selected}
        onClick={() => setSelected(!selected)}
      />
    );
  },
};

export const Group: Story = {
  render: () => {
    const [sel, setSel] = useState<string[]>(["europe"]);
    const groups = [
      { id: "europe", label: "europe", count: 12 },
      { id: "asia", label: "asia-pacific", count: 8 },
      { id: "us-east", label: "us-east", count: 6 },
      { id: "default", label: "default", count: 3 },
    ];
    const toggle = (id: string) =>
      setSel((s) => (s.includes(id) ? s.filter((x) => x !== id) : [...s, id]));
    return (
      <div className="flex flex-wrap gap-2">
        {groups.map((g) => (
          <ChipToggle
            key={g.id}
            label={g.label}
            sublabel={`${g.count} nodes`}
            selected={sel.includes(g.id)}
            onClick={() => toggle(g.id)}
          />
        ))}
      </div>
    );
  },
};
