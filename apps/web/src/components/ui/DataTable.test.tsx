import { render, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import type { DataColumn, DataRow } from "./DataTable";
import { DataTable } from "./DataTable";

const COLUMNS: DataColumn[] = [
  { key: "vmid", header: "ID", numeric: true, tone: "secondary" },
  { key: "name", header: "Nom" },
  { key: "size", header: "Taille", align: "right", numeric: true, nowrap: true },
];

const ROWS: DataRow[] = [
  { key: "1", cells: { vmid: 103, name: "sli-airflow", size: "32 Gio" } },
  { key: "2", cells: { vmid: 104, name: "template-rocky10", size: "8 Gio" } },
];

function show(props: Partial<React.ComponentProps<typeof DataTable>> = {}) {
  return render(
    <DataTable
      caption="Invités hébergés par ce nœud"
      columns={COLUMNS}
      rows={ROWS}
      emptyHint="Aucun invité sur ce nœud."
      {...props}
    />,
  );
}

describe("DataTable", () => {
  it("lists every row under its heading", () => {
    show();

    const table = screen.getByRole("table");
    expect(within(table).getAllByRole("row")).toHaveLength(3);
    expect(within(table).getByText("sli-airflow")).toBeInTheDocument();
    expect(within(table).getByText("template-rocky10")).toBeInTheDocument();
  });

  // A screen reader lands on a table with no title otherwise; sighted readers
  // have the heading above it, so the caption is hidden but never omitted.
  it("names itself for a screen reader", () => {
    show();

    expect(
      screen.getByRole("table", { name: "Invités hébergés par ce nœud" }),
    ).toBeInTheDocument();
  });

  // Without scope, a header cell says nothing about which cells it heads, and
  // a row read out of order loses its labels.
  it("scopes every header to its column", () => {
    show();

    const headers = screen.getAllByRole("columnheader");
    expect(headers).toHaveLength(3);
    for (const header of headers) {
      expect(header).toHaveAttribute("scope", "col");
    }
    expect(headers[0]).toHaveTextContent("ID");
  });

  it("leaves a column with nothing to name without a heading", () => {
    show({
      columns: [...COLUMNS, { key: "arrow" }],
      rows: [{ key: "1", cells: { vmid: 103, name: "x", size: "1 Gio", arrow: "→" } }],
    });

    const headers = screen.getAllByRole("columnheader");
    expect(headers[3]).toHaveTextContent("");
  });

  it("says so plainly rather than drawing an empty table", () => {
    show({ rows: [] });

    expect(screen.getByText("Aucun invité sur ce nœud.")).toBeInTheDocument();
    expect(screen.queryByRole("table")).toBeNull();
  });

  it("leaves an absent cell empty rather than failing", () => {
    show({ rows: [{ key: "1", cells: { name: "sans identifiant" } }] });

    const cells = screen.getAllByRole("cell");
    expect(cells).toHaveLength(3);
    expect(cells[0]).toHaveTextContent("");
    expect(cells[1]).toHaveTextContent("sans identifiant");
  });

  // Figures line up only in tabular numerals, and a size broken over two lines
  // is unreadable.
  it("applies the traits of a column to its cells", () => {
    show();

    const cells = screen.getAllByRole("cell");
    expect(cells[0]).toHaveClass("tabular-nums");
    expect(cells[0]).toHaveClass("text-text-secondary");
    expect(cells[2]).toHaveClass("text-right");
    expect(cells[2]).toHaveClass("whitespace-nowrap");
    expect(cells[1]).not.toHaveClass("tabular-nums");
  });

  it("aligns a header with its column", () => {
    show();

    const headers = screen.getAllByRole("columnheader");
    expect(headers[2]).toHaveClass("text-right");
    expect(headers[0]).toHaveClass("text-left");
  });

  // The last column has nothing to its right to be separated from, and the
  // padding pushed a right-aligned size away from the edge.
  it("drops the right padding of the last column", () => {
    show();

    const cells = screen.getAllByRole("cell");
    expect(cells[0]).toHaveClass("pr-3");
    expect(cells[2]).not.toHaveClass("pr-3");
    expect(screen.getAllByRole("columnheader")[2]).not.toHaveClass("pr-3");
  });

  // Automatic layout shares the slack between every column, so a heading wider
  // than all its figures is paid for by the column worth reading in full. One
  // column takes the spare width and the others shrink to their contents.
  it("gives the spare width to the column that asks for it", () => {
    show({
      columns: [
        { key: "vmid", header: "ID", numeric: true },
        { key: "name", header: "Nom", fill: true },
        { key: "size", header: "Taille", align: "right" },
      ],
    });

    const cells = screen.getAllByRole("cell");
    expect(cells[1]).toHaveClass("w-full");
    expect(cells[0]).not.toHaveClass("w-full");
    expect(cells[2]).not.toHaveClass("w-full");
    // The header of the column sizes it too, and a header narrower than its
    // cells would otherwise argue for a different width.
    expect(screen.getAllByRole("columnheader")[1]).toHaveClass("w-full");
  });

  // The hairline that already divides the rows, turned on its side: no new
  // colour, and padding on both sides so it sits in the middle of the gap.
  it("rules a column off from the one before, on demand", () => {
    show({
      columns: [
        { key: "vmid", header: "ID" },
        { key: "name", header: "Nom" },
        { key: "size", header: "Taille", divider: true },
      ],
    });

    const cells = screen.getAllByRole("cell");
    expect(cells[2]).toHaveClass("border-l-[0.5px]");
    expect(cells[2]).toHaveClass("border-border");
    expect(cells[2]).toHaveClass("pl-3");
    expect(cells[1]).not.toHaveClass("border-l-[0.5px]");
    expect(screen.getAllByRole("columnheader")[2]).toHaveClass("border-l-[0.5px]");
    expect(screen.getAllByRole("columnheader")[1]).not.toHaveClass("border-l-[0.5px]");
  });

  // Opt-in, and not the default: the five tables that predate it are snug
  // enough to read without one, and would only gain a grid.
  it("leaves a column undivided unless it asks", () => {
    show();

    for (const cell of screen.getAllByRole("cell")) {
      expect(cell).not.toHaveClass("border-l-[0.5px]");
      expect(cell).not.toHaveClass("w-full");
    }
  });

  // A whole row read as an aside — the guests a drain leaves where they are —
  // is muted however its columns are normally written.
  it("lets a row override the tone of every column", () => {
    show({
      rows: [{ key: "1", tone: "muted", cells: { vmid: 103, name: "x", size: "1 Gio" } }],
    });

    for (const cell of screen.getAllByRole("cell")) {
      expect(cell).toHaveClass("text-text-muted");
      expect(cell).not.toHaveClass("text-text-secondary");
    }
  });

  it("carries the classes a row asks for", () => {
    show({
      rows: [
        {
          key: "1",
          className: "hover:bg-fill-ghost-selected",
          cells: { vmid: 103, name: "x", size: "1 Gio" },
        },
      ],
    });

    const row = screen.getAllByRole("row")[1];
    expect(row).toHaveClass("hover:bg-fill-ghost-selected");
    expect(row).toHaveClass("border-border");
  });

  // Only for a table inside a scrolling region: a header pinned to a page that
  // does not scroll paints its own background over nothing.
  it("pins the headers only when asked", () => {
    const { unmount } = show();
    expect(screen.getAllByRole("columnheader")[0]).not.toHaveClass("sticky");
    unmount();

    show({ stickyHeader: true });
    const header = screen.getAllByRole("columnheader")[0];
    expect(header).toHaveClass("sticky");
    expect(header).toHaveClass("bg-surface-2");
  });

  it("renders a cell that is a node, not only a string", () => {
    show({
      rows: [
        {
          key: "1",
          cells: {
            vmid: 103,
            name: <button type="button">Ouvrir 103</button>,
            size: "1 Gio",
          },
        },
      ],
    });

    expect(screen.getByRole("button", { name: "Ouvrir 103" })).toBeInTheDocument();
  });

  it("merges the className it receives, on the table and on the hint", () => {
    const { container, unmount } = show({ className: "mb-3" });
    expect(container.firstElementChild).toHaveClass("mb-3");
    expect(container.firstElementChild).toHaveClass("overflow-x-auto");
    unmount();

    const empty = show({ rows: [], className: "mb-3" });
    expect(empty.container.firstElementChild).toHaveClass("mb-3");
  });
});
