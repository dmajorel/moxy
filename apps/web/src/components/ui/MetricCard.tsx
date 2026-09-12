import type { ReactNode } from "react";

import { UsageBar } from "./UsageBar";

/**
 * The CPU / memory / storage / load average card: a small label, one large
 * value, a quieter detail next to it, and an optional fill bar underneath.
 *
 * Nothing is formatted here. `value` and `detail` are ready-to-display nodes,
 * so units, the French decimal comma and rounding stay in the formatting layer.
 */
export interface MetricCardProps {
  /** Plain text: it also names the bar for screen readers. */
  label: string;
  value: ReactNode;
  /** Unit or precision, e.g. "/ 8 GiB" or "· 6 vCPU". */
  detail?: ReactNode;
  /** When set, a UsageBar is drawn below. Ratio in [0,1]. */
  ratio?: number;
  /** Forwarded to the bar: above it, the fill turns amber. */
  threshold?: number;
  className?: string;
}

const BASE_CLASSES =
  "rounded-card border-[0.5px] border-border bg-surface-2 px-3 py-2.5";

export function MetricCard({
  label,
  value,
  detail,
  ratio,
  threshold,
  className,
}: MetricCardProps) {
  const classes = [BASE_CLASSES, className].filter(Boolean).join(" ");

  return (
    <div className={classes}>
      <p className="mb-1 text-[11px] text-text-secondary">{label}</p>
      <p className="text-[20px] leading-[26px] font-medium text-text-primary">
        {value}
        {detail === undefined || detail === null ? null : (
          <span className="ml-1 text-[11px] font-normal text-text-muted">
            {detail}
          </span>
        )}
      </p>
      {ratio === undefined ? null : (
        <UsageBar
          className="mt-2"
          ratio={ratio}
          threshold={threshold}
          label={label}
        />
      )}
    </div>
  );
}
