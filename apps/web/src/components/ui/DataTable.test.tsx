import { render, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import type { DataTableColumn } from "./DataTable";
import { DataTable } from "./DataTable";

interface Row {
  id: number;
  name: string;
}

const ROWS: Row[] = [
  { id: 101, name: "sli-app" },
  { id: 102, name: "sli-db" },
];

const COLUMNS: DataTableColumn<Row>[] = [
  { header: "ID", cellClassName: "tabular-nums", render: (row) => row.id },
  { header: "Nom", render: (row) => row.name },
  { header: "", render: () => null },
];

function table(): HTMLElement {
  return screen.getByRole("table");
}

describe("DataTable", () => {
  it("names the table for a screen reader that lands on it", () => {
    render(
      <DataTable caption="Invités hébergés par ce nœud" columns={COLUMNS} rows={ROWS} rowKey={(row) => row.id} />,
    );

    expect(within(table()).getByText("Invités hébergés par ce nœud")).toBeInTheDocument();
  });

  // Without scope, a heading belongs to nothing: a screen reader cannot say
  // which column a cell is in.
  it("scopes every heading to its column", () => {
    render(<DataTable caption="Tableau" columns={COLUMNS} rows={ROWS} rowKey={(row) => row.id} />);

    const headers = within(table()).getAllByRole("columnheader");
    expect(headers).toHaveLength(3);
    for (const header of headers) {
      expect(header).toHaveAttribute("scope", "col");
    }
  });

  // Rows of unequal length tell a screen reader that a column has moved. Every
  // row emits one cell per column, even when the column has nothing to say.
  it("emits one cell per column on every row", () => {
    render(<DataTable caption="Tableau" columns={COLUMNS} rows={ROWS} rowKey={(row) => row.id} />);

    for (const row of within(table()).getAllByRole("row").slice(1)) {
      expect(within(row).getAllByRole("cell")).toHaveLength(COLUMNS.length);
    }
  });

  it("renders each row through its columns", () => {
    render(<DataTable caption="Tableau" columns={COLUMNS} rows={ROWS} rowKey={(row) => row.id} />);

    const row = screen.getByText("sli-db").closest("tr");
    expect(row).not.toBeNull();
    expect(within(row as HTMLElement).getByText("102")).toBeInTheDocument();
  });

  it("puts the hint in place of the table when there is no row", () => {
    render(
      <DataTable
        caption="Tableau"
        columns={COLUMNS}
        rows={[]}
        rowKey={(row) => row.id}
        emptyHint="Aucune tâche récente."
      />,
    );

    expect(screen.queryByRole("table")).toBeNull();
    expect(screen.getByText("Aucune tâche récente.")).toBeInTheDocument();
  });

  // A caller that explains above why the list is empty gets an empty table
  // rather than a second sentence saying the same thing.
  it("renders an empty table when no hint is given", () => {
    render(<DataTable caption="Tableau" columns={COLUMNS} rows={[]} rowKey={(row) => row.id} />);

    expect(table()).toBeInTheDocument();
    expect(within(table()).getAllByRole("row")).toHaveLength(1);
  });

  it("adds the classes a row asks for", () => {
    render(
      <DataTable
        caption="Tableau"
        columns={COLUMNS}
        rows={ROWS}
        rowKey={(row) => row.id}
        rowClassName={(row) => (row.id === 102 ? "text-text-muted" : undefined)}
      />,
    );

    expect(screen.getByText("sli-db").closest("tr")).toHaveClass("text-text-muted");
    expect(screen.getByText("sli-app").closest("tr")).not.toHaveClass("text-text-muted");
  });

  it("wraps the table in a horizontal scroller by default", () => {
    const { container } = render(
      <DataTable
        caption="Tableau"
        columns={COLUMNS}
        rows={ROWS}
        rowKey={(row) => row.id}
        className="mt-2"
      />,
    );

    expect(container.firstElementChild).toHaveClass("overflow-x-auto");
    expect(container.firstElementChild).toHaveClass("mt-2");
  });

  // A second scroll container is what a sticky heading would stick to, so the
  // table can be asked to stay bare inside a scroller of its own.
  it("renders the table bare when the scroller is turned off", () => {
    const { container } = render(
      <DataTable
        caption="Tableau"
        columns={COLUMNS}
        rows={ROWS}
        rowKey={(row) => row.id}
        scrollable={false}
        className="mb-3"
      />,
    );

    expect(container.firstElementChild?.tagName).toBe("TABLE");
    expect(container.firstElementChild).toHaveClass("mb-3");
  });
});
