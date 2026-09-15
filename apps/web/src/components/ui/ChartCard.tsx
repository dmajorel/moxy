import type { Series, Timeframe } from "@/api/types";
import { Sparkline } from "@/components/ui/Sparkline";
import { TimeframePicker } from "@/components/ui/TimeframePicker";
import { useFormat, useT } from "@/i18n/locale";
import { cpuRatios, timeTicks } from "@/lib/series";

/**
 * The CPU chart panel of the node and guest views.
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
  /** The window to draw, or null when it could not be read. */
  series: Series | null;
  /** Window the caller asked for; the payload still has the last word. */
  timeframe?: Timeframe;
  /** Given, the card offers the window picker. Omitted, it shows one window. */
  onTimeframeChange?: (timeframe: Timeframe) => void;
  className?: string;
}

export function ChartCard({
  title,
  label,
  series,
  timeframe = "hour",
  onTimeframeChange,
  className,
}: ChartCardProps) {
  const t = useT();
  const { formatRatio, formatTimeframe } = useFormat();
  const points = series?.points ?? [];
  // The legend names the window the points actually cover, not the button that
  // is lit: between the click and the answer the two disagree, and the chart
  // would otherwise claim a year while still drawing an hour.
  const shown = series?.timeframe ?? timeframe;

  return (
    <section
      className={[
        "rounded-card border-[0.5px] border-border bg-surface-2 px-3 py-2.5",
        className,
      ]
        .filter(Boolean)
        .join(" ")}
    >
      <div className="mb-1.5 flex flex-wrap items-center justify-between gap-x-3 gap-y-1">
        <div className="flex items-center gap-2">
          <h2 className="text-[12px] font-medium text-text-primary">{title}</h2>
          {onTimeframeChange !== undefined && (
            <TimeframePicker
              value={timeframe}
              onChange={onTimeframeChange}
              label={t("chart.windowLabel")}
            />
          )}
        </div>
        {/*
          The average is dropped rather than shown as a dash: a legend reading
          "moy. —" spends the line saying that the line above it is empty,
          which the empty chart already says.
        */}
        <span className="text-[11px] text-text-muted">
          {series === null
            ? formatTimeframe(shown)
            : t("chart.average", {
                timeframe: formatTimeframe(shown),
                value: formatRatio(series.cpuAverage),
              })}
        </span>
      </div>
      <Sparkline
        series={[{ values: cpuRatios(points) }]}
        label={label}
        // The "11:00 · 11:30 · 12:00" of appendix A.1: a chart with no time
        // axis does not say when the spike it shows happened. Past the day the
        // marks carry a date instead — an hour tells nothing about where a
        // sample sits in a month.
        ticks={timeTicks(points, shown)}
      />
    </section>
  );
}
