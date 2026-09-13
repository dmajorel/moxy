import type { Series } from "@/api/types";
import { Sparkline } from "@/components/ui/Sparkline";
import { formatRatio } from "@/lib/format";
import { cpuRatios, timeTicks } from "@/lib/series";

/**
 * The "last hour of CPU" panel of the node and guest views.
 *
 * Both screens drew it themselves, in thirty identical lines each: the same
 * card, the same legend, the same conversion of RRD points into ratios, the
 * same time marks. Two copies of a chart are two chances of one of them
 * auto-scaling, which is the defect of the native interface this whole
 * component exists to correct.
 *
 * The scale stays the Sparkline default — a share of full load, floor to
 * ceiling — and is never derived from the points.
 */
export interface ChartCardProps {
  /** Heading of the card, French sentence case. */
  title: string;
  /**
   * Accessible name of the chart itself, which names the object: "Charge CPU
   * de prox-qual-2201-cit". The heading above says what, this says of what.
   */
  label: string;
  /** The hour to draw, or null when it could not be read. */
  series: Series | null;
  className?: string;
}

export function ChartCard({ title, label, series, className }: ChartCardProps) {
  const points = series?.points ?? [];

  return (
    <section
      className={[
        "rounded-card border-[0.5px] border-border bg-surface-2 px-3 py-2.5",
        className,
      ]
        .filter(Boolean)
        .join(" ")}
    >
      <div className="mb-1.5 flex items-center justify-between gap-3">
        <h2 className="text-[12px] font-medium text-text-primary">{title}</h2>
        {/*
          The average is dropped rather than shown as a dash: a legend reading
          "moy. —" spends the line saying that the line above it is empty,
          which the empty chart already says.
        */}
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
