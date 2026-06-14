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
    // U3: the sort affordance is now a keyboard-operable <button> inside
    // the <th>; click it, but assert aria-sort on the owning columnheader.
    const nameButton = screen.getByRole("button", { name: /name/i });

    await user.click(nameButton);
    expect(nameHeader).toHaveAttribute("aria-sort", "ascending");

    await user.click(nameButton);
    expect(nameHeader).toHaveAttribute("aria-sort", "descending");
  });

  it("resets previously sorted column to aria-sort=none when a different column becomes active", async () => {
    const user = userEvent.setup();
    render(<DataTable columns={columns} data={rows} keyExtractor={(r) => r.id} />);
    const nameHeader = screen.getByRole("columnheader", { name: /name/i });
    const countHeader = screen.getByRole("columnheader", { name: /count/i });
    const nameButton = screen.getByRole("button", { name: /name/i });
    const countButton = screen.getByRole("button", { name: /count/i });

    await user.click(nameButton);
    expect(nameHeader).toHaveAttribute("aria-sort", "ascending");

    await user.click(countButton);
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
    const [firstAlpha] = screen.getAllByText("alpha");
    if (!firstAlpha) throw new Error("alpha cell missing");
    await user.click(firstAlpha);
    expect(onRowClick).toHaveBeenCalled();
  });
});

describe("DataTable sorting (reorders rows)", () => {
  const sortRows: Row[] = [
    { id: "1", name: "alpha", count: 10 },
    { id: "2", name: "beta", count: 5 },
  ];
  // Columns expose `sortValue` so the table knows what to compare on —
  // `render` returns ReactNode and can't be sorted on directly.
  const sortCols: DataTableColumn<Row>[] = [
    { key: "name", header: "Name", render: (r) => r.name, sortable: true, sortValue: (r) => r.name },
    {
      key: "count",
      header: "Count",
      render: (r) => String(r.count),
      sortable: true,
      sortValue: (r) => r.count,
    },
    { key: "static", header: "Info", render: () => "—" },
  ];

  // Both desktop and mobile views render the same sorted slice, so the
  // first "alpha"/"beta" cell in document order reflects the active order.
  function firstSortedName(): string {
    const matches = screen.getAllByText(/^(alpha|beta)$/);
    return matches[0]?.textContent ?? "";
  }

  it("reorders rows by the active sortable column and direction", async () => {
    const user = userEvent.setup();
    render(<DataTable columns={sortCols} data={sortRows} keyExtractor={(r) => r.id} />);
    // Default: untouched input order — alpha (count 10) first.
    expect(firstSortedName()).toBe("alpha");

    // Ascending by count -> beta (5) sorts before alpha (10).
    await user.click(screen.getByRole("button", { name: /count/i }));
    expect(firstSortedName()).toBe("beta");

    // Second click toggles descending -> alpha (10) before beta (5).
    await user.click(screen.getByRole("button", { name: /count/i }));
    expect(firstSortedName()).toBe("alpha");
  });

  it("sorts strings case-insensitively in ascending order", async () => {
    const user = userEvent.setup();
    const mixed: Row[] = [
      { id: "1", name: "beta", count: 1 },
      { id: "2", name: "Alpha", count: 2 },
    ];
    render(<DataTable columns={sortCols} data={mixed} keyExtractor={(r) => r.id} />);
    await user.click(screen.getByRole("button", { name: /name/i }));
    // "Alpha" should sort before "beta" despite the capital letter.
    const first = screen.getAllByText(/^(Alpha|beta)$/)[0]?.textContent ?? "";
    expect(first).toBe("Alpha");
  });

  it("exposes a mobile sort control that reorders the cards", async () => {
    const user = userEvent.setup();
    render(<DataTable columns={sortCols} data={sortRows} keyExtractor={(r) => r.id} />);
    // The mobile card view has no <thead>, so it carries its own sort
    // <select> covering the sortable columns.
    const sortSelect = screen.getByRole("combobox", { name: /sort/i });
    await user.selectOptions(sortSelect, "count");
    expect(firstSortedName()).toBe("beta");
  });
});

describe("DataTable mobile cards (column priority)", () => {
  it("omits cardHidden columns from the mobile card view", () => {
    const cols: DataTableColumn<Row>[] = [
      { key: "name", header: "NameCol", render: (r) => r.name },
      { key: "secret", header: "SecretCol", render: () => "x", cardHidden: true },
    ];
    render(<DataTable columns={cols} data={rows} keyExtractor={(r) => r.id} />);
    // Desktop <thead> renders "SecretCol" once; the mobile cards (2 rows)
    // would add two more label occurrences if it weren't hidden.
    expect(screen.getAllByText("SecretCol")).toHaveLength(1);
    // A normal column still appears in both desktop header and mobile cards.
    expect(screen.getAllByText("NameCol").length).toBeGreaterThan(1);
  });
});

// P3-PERF-02: DataTable must virtualize the desktop <tbody> so that
// rendering 5000 agents does not flood the DOM with 5000 <tr> nodes.
// jsdom reports 0 for clientHeight by default, which would collapse the
// virtualizer's viewport to zero rows. We stub the scroll parent's
// bounding metrics to produce a plausible 600px viewport so the
// virtualizer renders a realistic slice.
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

describe("DataTable virtualization (P3-PERF-02)", () => {
  // Bump per-test timeout to 15s — coverage instrumentation (v8 inlines
  // hit-counters into every render) makes mounting 5000 rows + the
  // virtualizer slow enough that the default 5s trips on CI runners
  // even though the uninstrumented `npm run test` finishes in ~50ms.
  it(
    "renders only a viewport-sized slice for 5000 rows",
    () => {
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
    },
    15_000,
  );
});
