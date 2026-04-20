import type { Meta, StoryObj } from "@storybook/react";
import { FormField } from "./form-field";
import { Input } from "./input";
import { Select } from "./select";
import { Toggle } from "./toggle";

const meta: Meta<typeof FormField> = {
  title: "UI/FormField",
  component: FormField,
};
export default meta;

type Story = StoryObj<typeof FormField>;

export const WithInput: Story = {
  render: () => (
    <FormField
      label="Server Name"
      description="Unique identifier for this node"
      className="w-[300px]"
    >
      <Input placeholder="node-eu-fra-01" />
    </FormField>
  ),
};

export const WithError: Story = {
  render: () => (
    <FormField label="Port" error="Port must be between 1024 and 65535" className="w-[300px]">
      <Input defaultValue="80" />
    </FormField>
  ),
};

export const WithSelect: Story = {
  render: () => (
    <FormField label="Region" className="w-[300px]">
      <Select
        options={[
          { value: "eu", label: "Europe" },
          { value: "us", label: "United States" },
          { value: "ap", label: "Asia Pacific" },
        ]}
        onChange={() => {}}
      />
    </FormField>
  ),
};

export const WithToggle: Story = {
  render: () => (
    <div className="flex items-center justify-between w-[300px]">
      <FormField label="Enable TLS" description="Encrypt client connections">
        <span />
      </FormField>
      <Toggle checked={true} onChange={() => {}} />
    </div>
  ),
};

export const UppercaseVariant: Story = {
  render: () => (
    <div className="flex flex-col gap-4 w-[300px]">
      <FormField label="Client Name" variant="uppercase" required>
        <Input placeholder="e.g. premium-users" />
      </FormField>
      <FormField
        label="Ad Tag"
        variant="uppercase"
        description="Promotional channel displayed to users"
      >
        <Input placeholder="Telegram ad channel tag" />
      </FormField>
    </div>
  ),
};

export const CompactVariant: Story = {
  render: () => (
    <div className="grid grid-cols-3 gap-3 w-[400px]">
      <FormField label="Max TCP Conns" variant="compact">
        <Input type="number" placeholder="0" className="font-mono text-xs" />
      </FormField>
      <FormField label="Max Unique IPs" variant="compact">
        <Input type="number" placeholder="0" className="font-mono text-xs" />
      </FormField>
      <FormField label="Data Quota" variant="compact">
        <Input type="number" placeholder="0" className="font-mono text-xs" />
      </FormField>
    </div>
  ),
};

export const RequiredWithError: Story = {
  render: () => (
    <FormField
      label="Username"
      variant="uppercase"
      required
      error="Username is already taken"
      className="w-[300px]"
    >
      <Input value="admin" />
    </FormField>
  ),
};
