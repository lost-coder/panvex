import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { DataTable, type DataTableColumn } from "./DataTable";

interface Row {
  id: string;
  name: string;
  count: number;
}

const rows: Row[] = [
  { id: "1", name: "alpha", count: 10 },
  { id: "2", name: "beta", count: 5 },
];

const columns: DataTableColumn<Row>[] = [
  { key: "name", header: "Name", render: (r) => r.name, sortable: true },
  { key: "count", header: "Count", render: (r) => String(r.count), sortable: true },
  { key: "static", header: "Info", render: () => "—" },
];

describe("DataTable a11y (P2-FE-07 / M-F6)", () => {
  it("marks sortable headers with scope=col and aria-sort=none by default", () => {
    render(<DataTable columns={columns} data={rows} keyExtractor={(r) => r.id} />);
    const nameHeader = screen.getByRole("columnheader", { name: /name/i });
    const countHeader = screen.getByRole("columnheader", { name: /count/i });
    expect(nameHeader).toHaveAttribute("scope", "col");
    expect(nameHeader).toHaveAttribute("aria-sort", "none");
    expect(countHeader).toHaveAttribute("aria-sort", "none");
  });

  it("omits aria-sort on non-sortable columns", () => {
    render(<DataTable columns={columns} data={rows} keyExtractor={(r) => r.id} />);
    const staticHeader = screen.getByRole("columnheader", { name: /info/i });
    // scope=col still applied for semantic structure
    expect(staticHeader).toHaveAttribute("scope", "col");
    expect(staticHeader).not.toHaveAttribute("aria-sort");
  });

  it("flips aria-sort between ascending and descending on consecutive clicks", async () => {
    const user = userEvent.setup();
    render(<DataTable columns={columns} data={rows} keyExtractor={(r) => r.id} />);
    const nameHeader = screen.getByRole("columnheader", { name: /name/i });

    await user.click(nameHeader);
    expect(nameHeader).toHaveAttribute("aria-sort", "ascending");

    await user.click(nameHeader);
    expect(nameHeader).toHaveAttribute("aria-sort", "descending");
  });

  it("resets previously sorted column to aria-sort=none when a different column becomes active", async () => {
    const user = userEvent.setup();
    render(<DataTable columns={columns} data={rows} keyExtractor={(r) => r.id} />);
    const nameHeader = screen.getByRole("columnheader", { name: /name/i });
    const countHeader = screen.getByRole("columnheader", { name: /count/i });

    await user.click(nameHeader);
    expect(nameHeader).toHaveAttribute("aria-sort", "ascending");

    await user.click(countHeader);
    expect(countHeader).toHaveAttribute("aria-sort", "ascending");
    expect(nameHeader).toHaveAttribute("aria-sort", "none");
  });

  it("renders empty message when data is empty", () => {
    render(
      <DataTable
        columns={columns}
        data={[]}
        keyExtractor={(r) => r.id}
        emptyMessage="Nothing here"
      />,
    );
    // Both desktop and mobile copies render; queryAllByText verifies at least one exists.
    expect(screen.getAllByText("Nothing here").length).toBeGreaterThan(0);
  });

  it("invokes onRowClick when row is clicked", async () => {
    const user = userEvent.setup();
    const onRowClick = vi.fn();
    render(
      <DataTable
        columns={columns}
        data={rows}
        keyExtractor={(r) => r.id}
        onRowClick={onRowClick}
      />,
    );
    // Click the desktop <tr> via the visible "alpha" cell.
    const alphaCells = screen.getAllByText("alpha");
    expect(alphaCells.length).toBeGreaterThan(0);
    await user.click(alphaCells[0]!);
    expect(onRowClick).toHaveBeenCalled();
  });
});

// P3-PERF-02: DataTable must virtualize the desktop <tbody> so that
// rendering 5000 agents does not flood the DOM with 5000 <tr> nodes.
// jsdom reports 0 for clientHeight by default, which would collapse the
// virtualizer's viewport to zero rows. We stub the scroll parent's
// bounding metrics to produce a plausible 600px viewport so the
// virtualizer renders a realistic slice.
describe("DataTable virtualization (P3-PERF-02)", () => {
  function stubViewport(height: number) {
    Object.defineProperty(HTMLElement.prototype, "clientHeight", {
      configurable: true,
      get() {
        return height;
      },
    });
    Object.defineProperty(HTMLElement.prototype, "offsetHeight", {
      configurable: true,
      get() {
        return height;
      },
    });
  }

  it("renders only a viewport-sized slice for 5000 rows", () => {
    stubViewport(600);
    const bigData: Row[] = Array.from({ length: 5000 }, (_, i) => ({
      id: String(i),
      name: `row-${i}`,
      count: i,
    }));
    render(
      <DataTable
        columns={columns}
        data={bigData}
        keyExtractor={(r) => r.id}
        rowHeight={48}
        maxHeight={600}
      />,
    );
    // Desktop table body rows only (excluding header + spacer rows).
    // With a 600px viewport and 48px rows that's ~13 visible rows +
    // overscan (6 on each side) => at most ~26 real data rows.
    const bodyRows = document.querySelectorAll<HTMLTableRowElement>("tbody tr[data-index]");
    expect(bodyRows.length).toBeGreaterThan(0);
    expect(bodyRows.length).toBeLessThan(40);
  });
});
