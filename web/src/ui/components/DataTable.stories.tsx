import type { Meta, StoryObj } from "@storybook/react";
import { DataTable, type DataTableColumn } from "./DataTable";
import { ConnectionBadge } from "@/ui/primitives/ConnectionBadge";
import { TrafficCell } from "@/ui/primitives/TrafficCell";

const meta: Meta = {
  title: "Components/DataTable",
};
export default meta;

type Story = StoryObj;

interface User {
  id: string;
  name: string;
  online: boolean;
  connections: number;
  traffic: number;
}

const users: User[] = [
  { id: "1", name: "admin_main", online: true, connections: 5, traffic: 3_200_000_000 },
  { id: "2", name: "proxy_user_42", online: true, connections: 2, traffic: 2_480_000_000 },
  { id: "3", name: "test_bot", online: false, connections: 0, traffic: 1_200_000 },
  { id: "4", name: "alice_vpn", online: true, connections: 1, traffic: 890_000_000 },
  { id: "5", name: "backup_svc", online: false, connections: 0, traffic: 45_000_000 },
];

const columns: DataTableColumn<User>[] = [
  {
    key: "name",
    header: "User",
    sortable: true,
    render: (r) => <span className="font-mono text-fg">{r.name}</span>,
  },
  {
    key: "status",
    header: "Status",
    render: (r) => <ConnectionBadge online={r.online} count={r.connections} />,
  },
  {
    key: "traffic",
    header: "Traffic",
    sortable: true,
    render: (r) => <TrafficCell bytes={r.traffic} />,
  },
];

export const Default: Story = {
  render: () => (
    <div className="w-[600px]">
      <DataTable columns={columns} data={users} keyExtractor={(r) => r.id} />
    </div>
  ),
};

export const Empty: Story = {
  render: () => (
    <div className="w-[600px]">
      <DataTable
        columns={columns}
        data={[]}
        keyExtractor={(r: User) => r.id}
        emptyMessage="No users found"
      />
    </div>
  ),
};
