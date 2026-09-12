import type { ReactNode } from "react";

/**
 * Compact key/value panel used beside the charts.
 *
 * Section 2 demotes these lists deliberately: status and metrics come first,
 * and this is where the remaining facts live — kernel, quorum, IP address.
 */
export interface KeyValueRow {
  label: string;
  /** A nullish value renders the em dash: unknown is never shown as empty. */
  value: ReactNode;
  /** Renders the value in the monospace face, for versions and addresses. */
  mono?: boolean;
}

export interface KeyValueProps {
  rows: KeyValueRow[];
  className?: string;
}

export function KeyValue({ rows, className }: KeyValueProps) {
  return (
    <dl className={["m-0 grid", className].filter(Boolean).join(" ")}>
      {rows.map((row, index) => (
        <div
          key={row.label}
          className={[
            "flex items-baseline justify-between gap-3 py-[7px] text-[13px]",
            index === rows.length - 1 ? "" : "border-b-[0.5px] border-border",
          ]
            .filter(Boolean)
            .join(" ")}
        >
          <dt className="text-text-secondary">{row.label}</dt>
          <dd
            className={[
              "m-0 text-right text-text-primary",
              row.mono === true ? "font-mono text-[12px]" : "",
            ]
              .filter(Boolean)
              .join(" ")}
          >
            {row.value === null || row.value === undefined || row.value === "" ? (
              <span className="text-text-muted">—</span>
            ) : (
              row.value
            )}
          </dd>
        </div>
      ))}
    </dl>
  );
}
