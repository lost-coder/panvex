import type { Meta, StoryObj } from "@storybook/react";
import { SettingsGroup } from "./SettingsGroup";
import { SettingsRow } from "./SettingsRow";
import { Toggle } from "@/ui/base/toggle";
import { Select } from "@/ui/base/select";
import { Input } from "@/ui/base/input";

const meta: Meta<typeof SettingsGroup> = {
  title: "Components/SettingsGroup",
  component: SettingsGroup,
};
export default meta;

type Story = StoryObj<typeof SettingsGroup>;

export const Default: Story = {
  render: () => (
    <div className="w-[400px]">
      <SettingsGroup title="Connection" description="Configure proxy connection settings">
        <SettingsRow label="Enable TLS" description="Encrypt all client traffic">
          <Toggle checked={true} onChange={() => {}} />
        </SettingsRow>
        <SettingsRow label="Max connections" description="Per-user connection limit">
          <Input defaultValue="10" className="w-20 text-right" />
        </SettingsRow>
        <SettingsRow label="Protocol" description="MTProto protocol version">
          <Select
            options={[
              { value: "abridged", label: "Abridged" },
              { value: "intermediate", label: "Intermediate" },
              { value: "full", label: "Full" },
            ]}
            value="intermediate"
            onChange={() => {}}
            className="w-[140px]"
          />
        </SettingsRow>
      </SettingsGroup>
    </div>
  ),
};
