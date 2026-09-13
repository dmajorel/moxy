import type { ReactNode } from "react";

/**
 * The one table of the interface.
 *
 * Five tables — recent tasks, the guests of a node, its pending packages, the
 * volumes of a guest, the moves of a maintenance plan — each carried their own
 * `<thead>`, their own padding on every cell and their own screen-reader
 * caption. The accessibility of a table lives in exactly those places: the
 * `scope` of a header cell, the caption that names it, the header that stays
 * put while the body scrolls. Five copies meant five chances of forgetting one,
 * and the pending-packages table had already lost its right-hand padding.
 *
 * Columns carry what is true of a whole column — its heading, its alignment,
 * whether its figures line up — and rows carry only content. Nothing here
 * formats: a cell receives what `lib/format` has already written.
 */
export type ColumnAlign = "left" | "right";

/** Which text colour a column, or a whole row, is written in. */
export type CellTone = "primary" | "secondary" | "muted";

const TONE_CLASSES: Record<CellTone, string> = {
  primary: "text-text-primary",
  secondary: "text-text-secondary",
  muted: "text-text-muted",
};

export interface DataColumn {
  /** Stable key, and the key of this column's cell in every row. */
  key: string;
  /**
   * Heading, French sentence case. Omitted for a column that holds nothing but
   * an icon — the arrow of the maintenance plan — which has nothing to name.
   */
  header?: string;
  align?: ColumnAlign;
  /** Figures line up only in tabular numerals. */
  numeric?: boolean;
  /** Identifiers, versions and volume names, which are read character by character. */
  mono?: boolean;
  /** Never wrapped: a size or a timestamp broken over two lines is unreadable. */
  nowrap?: boolean;
  tone?: CellTone;
  /** Anything else this column needs on its cells, e.g. `break-all`. */
  className?: string;
}

export interface DataRow {
  key: string;
  /** One entry per column key. A missing one leaves the cell empty. */
  cells: Record<string, ReactNode>;
  /** Overrides the tone of every column, for a row read as an aside. */
  tone?: CellTone;
  /** Anything else this row needs, e.g. a hover. */
  className?: string;
}

export interface DataTableProps {
  /**
   * Names the table for a screen reader, which otherwise lands on a table with
   * no title. Sighted readers have the heading above it, so it is visually
   * hidden — but it is never omitted.
   */
  caption: string;
  columns: DataColumn[];
  rows: DataRow[];
  /** Shown instead of the table when there is nothing to list. */
  emptyHint: string;
  /**
   * Keeps the headers visible while the body scrolls under them. For a table
   * inside a scrolling region only: a header pinned to a page that does not
   * scroll simply paints its own background over nothing.
   */
  stickyHeader?: boolean;
  className?: string;
}

export function DataTable({
  caption,
  columns,
  rows,
  emptyHint,
  stickyHeader,
  className,
}: DataTableProps) {
  if (rows.length === 0) {
    return (
      <p className={join("text-[12px] text-text-muted", className)}>{emptyHint}</p>
    );
  }

  return (
    <div className={join("overflow-x-auto", className)}>
      <table className="w-full border-collapse text-[12px]">
        <caption className="sr-only">{caption}</caption>
        <thead>
          <tr className="text-[11px] text-text-muted">
            {columns.map((column, index) => (
              <th
                key={column.key}
                scope="col"
                className={join(
                  "py-1.5 font-normal",
                  index === columns.length - 1 ? null : "pr-3",
                  column.align === "right" ? "text-right" : "text-left",
                  stickyHeader === true ? "sticky top-0 bg-surface-2" : null,
                )}
              >
                {column.header}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {rows.map((row) => (
            <tr
              key={row.key}
              className={join("border-t-[0.5px] border-border", row.className)}
            >
              {columns.map((column, index) => (
                <td
                  key={column.key}
                  className={join(
                    "py-1.5",
                    index === columns.length - 1 ? null : "pr-3",
                    column.align === "right" ? "text-right" : null,
                    column.numeric === true ? "tabular-nums" : null,
                    column.mono === true ? "font-mono" : null,
                    column.nowrap === true ? "whitespace-nowrap" : null,
                    // The row wins: a whole row read as an aside is muted
                    // however its columns are normally written.
                    TONE_CLASSES[row.tone ?? column.tone ?? "primary"],
                    column.className,
                  )}
                >
                  {row.cells[column.key]}
                </td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function join(...classes: (string | null | undefined)[]): string {
  return classes.filter(Boolean).join(" ");
}
