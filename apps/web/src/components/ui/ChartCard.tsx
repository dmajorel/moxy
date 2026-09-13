import type { Series } from "@/api/types";
import { formatRatio } from "@/lib/format";
import { cpuRatios, timeTicks } from "@/lib/series";

import { Sparkline } from "./Sparkline";

/**
 * The "Charge CPU · dernière heure" panel of the node and guest views.
 *
 * Heading, the average in the legend, the fixed-scale chart and its time marks
 * were written twice, once per detail screen, down to the class strings. They
 * draw the same thing about the same kind of object, and an operator comparing
 * a node with one of its guests must not be reading two charts built by two
 * pieces of code.
 *
 * The window is always named, even when the series could not be read: a chart
 * with no legend does not say what hour it covers, and an average that is
 * missing is not an average of zero.
 */
export interface ChartCardProps {
  /** Heading of the panel. French, sentence case. */
  title: string;
  /** Accessible name of the curve, e.g. "Charge CPU de pve-01". */
  label: string;
  /** The hour to draw. `null` when it could not be read. */
  series: Series | null;
  className?: string;
}

const BASE_CLASSES =
  "rounded-card border-[0.5px] border-border bg-surface-2 px-3 py-2.5";

export function ChartCard({ title, label, series, className }: ChartCardProps) {
  const points = series?.points ?? [];
  const classes = [BASE_CLASSES, className].filter(Boolean).join(" ");

  return (
    <section className={classes}>
      <div className="mb-1.5 flex items-center justify-between gap-3">
        <h2 className="text-[12px] font-medium text-text-primary">{title}</h2>
        <span className="text-[11px] text-text-muted">
          {series === null
            ? "Dernière heure"
            : `Dernière heure · moy. ${formatRatio(series.cpuAverage)}`}
        </span>
      </div>
      <Sparkline
        series={[{ values: cpuRatios(points) }]}
        label={label}
        // The "11:00 · 11:30 · 12:00" of appendix A.1: a chart with no time
        // axis does not say when the spike it shows happened.
        ticks={timeTicks(points)}
      />
    </section>
  );
}
