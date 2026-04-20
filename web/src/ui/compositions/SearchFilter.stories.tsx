import { useState } from "react";
import type { Meta, StoryObj } from "@storybook/react";
import { SearchFilter } from "./SearchFilter";

const meta: Meta<typeof SearchFilter> = {
  title: "Compositions/SearchFilter",
  component: SearchFilter,
};
export default meta;

type Story = StoryObj<typeof SearchFilter>;

function Demo() {
  const [search, setSearch] = useState("");
  const [status, setStatus] = useState<string>();
  return (
    <SearchFilter
      searchValue={search}
      onSearchChange={setSearch}
      searchPlaceholder="Search users…"
      filters={[
        {
          key: "status",
          options: [
            { value: "all", label: "All" },
            { value: "online", label: "Online" },
            { value: "offline", label: "Offline" },
          ],
          value: status,
          onChange: setStatus,
          placeholder: "Status",
        },
      ]}
      className="w-[500px]"
    />
  );
}

export const Default: Story = { render: () => <Demo /> };

function SearchOnly() {
  const [search, setSearch] = useState("");
  return (
    <SearchFilter
      searchValue={search}
      onSearchChange={setSearch}
      searchPlaceholder="Search nodes…"
      className="w-[300px]"
    />
  );
}

export const SearchOnlyVariant: Story = { render: () => <SearchOnly /> };
