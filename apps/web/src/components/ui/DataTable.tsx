import type { ReactNode } from "react";

/**
 * The one table of this interface.
 *
 * Four of them were written by hand — recent tasks, the guests of a node, its
 * pending packages, the guests of a drain plan — each repeating the same class
 * strings, the same `<thead>`, and each carrying its own copy of the details
 * that decide whether a screen reader can read it: the `scope` of a heading,
 * the `caption` a table with no title needs, a row whose cells all line up.
 * They drifted, as four copies do: the plan table emitted five cells under six
 * headings, which tells a screen reader that a column has moved.
 *
 * Padding and rules belong here, not to the caller. A column says what it
 * holds and how its content aligns; it does not get to decide that its table
 * is a little tighter than the others.
 */
export interface DataTableColumn<Row> {
  /** Heading text. Empty for a column that carries none — an arrow, an icon. */
  header: string;
  /** Extra classes for the heading cell alone, e.g. a sticky offset. */
  headClassName?: string;
  /** Extra classes for the body cells of this column: alignment, colour, font. */
  cellClassName?: string;
  render: (row: Row) => ReactNode;
}

export interface DataTableProps<Row> {
  /**
   * Named for a screen reader, which lands on a table with no title otherwise.
   * Sighted readers have the heading above it.
   */
  caption: string;
  columns: DataTableColumn<Row>[];
  rows: Row[];
  /** Stable identity of a row. */
  rowKey: (row: Row) => string | number;
  /** Extra classes on a row, e.g. the hover of a row one can open. */
  rowClassName?: (row: Row) => string | undefined;
  /**
   * Shown in place of the table when there is no row. Omitted, an empty table
   * is rendered — which is what a caller wants when it says something else
   * above about why the list is empty.
   */
  emptyHint?: string;
  /**
   * Wraps the table in a horizontal scroller, which is what a narrow window
   * needs. Turn it off inside a scroller of its own: a second scroll container
   * is what a sticky heading sticks to, and it would stop sticking to the one
   * that matters.
   */
  scrollable?: boolean;
  className?: string;
}

export function DataTable<Row>({
  caption,
  columns,
  rows,
  rowKey,
  rowClassName,
  emptyHint,
  scrollable = true,
  className,
}: DataTableProps<Row>) {
  if (rows.length === 0 && emptyHint !== undefined) {
    return (
      <p className={join("text-[12px] text-text-muted", className)}>{emptyHint}</p>
    );
  }

  const last = columns.length - 1;
  const table = (
    <table className={join("w-full border-collapse text-[12px]", scrollable ? undefined : className)}>
      <caption className="sr-only">{caption}</caption>
      <thead>
        <tr className="text-left text-[11px] text-text-muted">
          {columns.map((column, index) => (
            <th
              key={column.header === "" ? `column-${String(index)}` : column.header}
              scope="col"
              className={join(
                "py-1.5 font-normal",
                index === last ? undefined : "pr-3",
                column.headClassName,
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
            key={rowKey(row)}
            className={join("border-t-[0.5px] border-border", rowClassName?.(row))}
          >
            {columns.map((column, index) => (
              <td
                key={column.header === "" ? `column-${String(index)}` : column.header}
                className={join(
                  "py-1.5",
                  index === last ? undefined : "pr-3",
                  column.cellClassName,
                )}
              >
                {column.render(row)}
              </td>
            ))}
          </tr>
        ))}
      </tbody>
    </table>
  );

  return scrollable ? (
    <div className={join("overflow-x-auto", className)}>{table}</div>
  ) : (
    table
  );
}

function join(...classes: (string | undefined)[]): string {
  return classes.filter(Boolean).join(" ");
}
