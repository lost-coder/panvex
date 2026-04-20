import type { Meta, StoryObj } from "@storybook/react";
import * as React from "react";
import { TableView } from "./TableView";

const meta: Meta<typeof TableView> = {
  title: "Compositions/TableView",
  component: TableView,
  parameters: { layout: "padded" },
};
export default meta;

type Story = StoryObj<typeof TableView>;

// ---------------------------------------------------------------------------
// Controlled wrapper
// ---------------------------------------------------------------------------
function TableViewDemo({
  totalPages = 3,
  totalItems = 28,
  pageSize = 10,
}: {
  totalPages?: number;
  totalItems?: number;
  pageSize?: number;
}) {
  const [search, setSearch] = React.useState("");
  const [status, setStatus] = React.useState("");
  const [group, setGroup] = React.useState("");
  const [viewMode, setViewMode] = React.useState<"cards" | "list">("cards");
  const [page, setPage] = React.useState(1);
  const [colVis, setColVis] = React.useState<Record<string, boolean>>({
    status: true,
    ip: true,
    group: true,
    lastSeen: true,
  });

  const handleColChange = (key: string, visible: boolean) =>
    setColVis((prev) => ({ ...prev, [key]: visible }));

  const rangeStart = (page - 1) * pageSize + 1;
  const rangeEnd = Math.min(page * pageSize, totalItems);

  return (
    <TableView
      search={{ value: search, onChange: setSearch, placeholder: "Search servers…" }}
      filters={[
        {
          key: "status",
          value: status,
          onChange: setStatus,
          placeholder: "All statuses",
          options: [
            { value: "online", label: "Online" },
            { value: "offline", label: "Offline" },
            { value: "degraded", label: "Degraded" },
          ],
        },
        {
          key: "group",
          value: group,
          onChange: setGroup,
          placeholder: "All groups",
          options: [
            { value: "prod", label: "Production" },
            { value: "staging", label: "Staging" },
            { value: "dev", label: "Development" },
          ],
        },
      ]}
      viewMode={{ current: viewMode, onChange: setViewMode }}
      columns={{
        available: [
          { key: "status", label: "Status" },
          { key: "ip", label: "IP Address" },
          { key: "group", label: "Group" },
          { key: "lastSeen", label: "Last Seen" },
        ],
        visibility: colVis,
        onChange: handleColChange,
      }}
      pagination={{ page, totalPages, totalItems, pageSize, onChange: setPage }}
    >
      {/* Placeholder content */}
      <div className="rounded-xl border border-border bg-bg-card p-6 flex flex-col gap-3">
        <p className="text-sm text-fg-muted font-mono">
          view: <span className="text-fg">{viewMode}</span> · search:{" "}
          <span className="text-fg">"{search || "—"}"</span> · status:{" "}
          <span className="text-fg">{status || "all"}</span> · group:{" "}
          <span className="text-fg">{group || "all"}</span>
        </p>
        <p className="text-xs text-fg-muted font-mono">
          page {page} · showing {rangeStart}–{rangeEnd} of {totalItems}
        </p>
        <p className="text-xs text-fg-muted font-mono">
          visible cols:{" "}
          {Object.entries(colVis)
            .filter(([, v]) => v)
            .map(([k]) => k)
            .join(", ")}
        </p>
        <div className="mt-2 grid grid-cols-2 sm:grid-cols-3 gap-2">
          {Array.from({ length: Math.min(pageSize, totalItems - (page - 1) * pageSize) }).map(
            (_, i) => (
              <div
                key={i}
                className="h-16 rounded-lg border border-border bg-bg flex items-center justify-center text-xs text-fg-muted font-mono"
              >
                item {(page - 1) * pageSize + i + 1}
              </div>
            ),
          )}
        </div>
      </div>
    </TableView>
  );
}

// ---------------------------------------------------------------------------
// Stories
// ---------------------------------------------------------------------------

export const Default: Story = {
  render: () => <TableViewDemo />,
};

export const SearchOnly: Story = {
  render: () => {
    const [search, setSearch] = React.useState("");
    return (
      <TableView search={{ value: search, onChange: setSearch, placeholder: "Search anything…" }}>
        <div className="rounded-xl border border-border bg-bg-card p-6 text-sm text-fg-muted font-mono">
          query: "{search || "—"}"
        </div>
      </TableView>
    );
  },
};

export const WithoutPagination: Story = {
  render: () => {
    const [search, setSearch] = React.useState("");
    const [status, setStatus] = React.useState("");
    const [viewMode, setViewMode] = React.useState<"cards" | "list">("list");
    return (
      <TableView
        search={{ value: search, onChange: setSearch }}
        filters={[
          {
            key: "status",
            value: status,
            onChange: setStatus,
            placeholder: "All statuses",
            options: [
              { value: "online", label: "Online" },
              { value: "offline", label: "Offline" },
            ],
          },
        ]}
        viewMode={{ current: viewMode, onChange: setViewMode }}
      >
        <div className="rounded-xl border border-border bg-bg-card p-6 text-sm text-fg-muted font-mono">
          No pagination · {viewMode} view
        </div>
      </TableView>
    );
  },
};

export const ManyPages: Story = {
  render: () => <TableViewDemo totalPages={10} totalItems={98} pageSize={10} />,
};
