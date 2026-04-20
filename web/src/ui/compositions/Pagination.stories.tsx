import { useState } from "react";
import type { Meta, StoryObj } from "@storybook/react";
import { Pagination } from "./Pagination";

const meta: Meta<typeof Pagination> = {
  title: "Compositions/Pagination",
  component: Pagination,
};
export default meta;

type Story = StoryObj<typeof Pagination>;

function Demo({ total }: { total: number }) {
  const [page, setPage] = useState(1);
  return <Pagination page={page} totalPages={total} onPageChange={setPage} />;
}

export const FewPages: Story = { render: () => <Demo total={5} /> };
export const ManyPages: Story = { render: () => <Demo total={20} /> };
export const SinglePage: Story = {
  render: () => <Pagination page={1} totalPages={1} onPageChange={() => {}} />,
};
