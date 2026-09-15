/**
 * Fixed-height chart.
 *
 * Section 2 is categorical about this one: the vertical scale is **never**
 * auto-fitted. The native interface rescales to the data, which turns a node
 * idling at 0.6% into a dramatic mountain range and makes every node look
 * equally busy. Here the axis is pinned to [0, `scaleMax`] so that a flat line
 * means "nothing is happening" and two charts side by side are comparable.
 *
 * RRD returns gaps, and `null` means unknown. A gap is not drawn as zero — that
 * would invent a drop that never happened — it splits the line instead.
 *
 * It takes ratios rather than payload points: the detail screens draw one curve
 * and a cluster card draws two, and turning a `Point` into a ratio is the job
 * of `lib/series.ts`, not of a chart.
 */
import { useT } from "@/i18n/locale";

/** One curve. `values` are ratios against `scaleMax`; null is a gap. */
export interface SparklineSeries {
  values: (number | null)[];
  /**
   * How the curve is drawn. `primary` is the area fill plus line of section 2;
   * `secondary` is a line alone in the muted tone, which is how a second curve
   * is told from the first without inventing a colour. Colour alone never
   * carries the distinction: the caller labels its curves.
   */
  tone?: SparklineTone;
}

export type SparklineTone = "primary" | "secondary";

export interface SparklineProps {
  series: SparklineSeries[];
  /**
   * Top of the vertical axis, as a ratio. The default of 1 means the chart
   * always reads as a share of full load; raise the floor only if a view
   * genuinely needs it, and never derive it from the data.
   */
  scaleMax?: number;
  /** Rendered height in pixels. Section 2 sets 70 for the detail screens. */
  height?: number;
  /** Accessible summary, e.g. "Charge CPU, moyenne 0,42 %". */
  label: string;
  /**
   * Time marks written under the chart, left to right — the "11:00 · 11:30 ·
   * 12:00" of appendix A.1. A chart with no time axis does not say WHEN the
   * spike it shows happened, which is the first thing asked of it.
   *
   * Built by `lib/series.timeTicks`, never here: a chart formats nothing.
   * Omitted on the cluster cards, which are 48 px tall and say "Dernière
   * heure" in their own legend.
   */
  ticks?: string[];
  className?: string;
}

const VIEW_WIDTH = 300;

/** Stroke and fill of each tone, as token names — never a literal colour. */
const TONES: Record<SparklineTone, { stroke: string; fill: string | null }> = {
  primary: { stroke: "var(--accent)", fill: "var(--bg-accent)" },
  secondary: { stroke: "var(--text-muted)", fill: null },
};

interface Sample {
  x: number;
  y: number;
}

export function Sparkline({
  series,
  scaleMax = 1,
  height = 70,
  label,
  ticks,
  className,
}: SparklineProps) {
  const t = useT();
  const curves = series.map((curve) => ({
    tone: curve.tone ?? "primary",
    segments: buildSegments(curve.values, scaleMax, height),
  }));
  const drawn = curves.some((curve) => curve.segments.length > 0);

  const classes = ["w-full", className].filter(Boolean).join(" ");

  if (!drawn) {
    return (
      <div
        className={[classes, "flex items-center justify-center text-[11px] text-text-muted"]
          .filter(Boolean)
          .join(" ")}
        style={{ height }}
        role="img"
        aria-label={t("chart.noDataLabel", { label })}
      >
        {t("chart.noData")}
      </div>
    );
  }

  const chart = (
    <svg
      className="w-full"
      viewBox={`0 0 ${String(VIEW_WIDTH)} ${String(height)}`}
      height={height}
      preserveAspectRatio="none"
      role="img"
      aria-label={label}
    >
      {curves.map((curve, curveIndex) => {
        const { stroke, fill } = TONES[curve.tone];
        const last = lastSample(curve.segments);

        return (
          <g key={curveIndex}>
            {curve.segments.map((segment, index) => (
              <g key={index}>
                {fill !== null && segment.length > 1 ? (
                  <polygon points={areaPoints(segment, height)} fill={fill} />
                ) : null}
                <polyline
                  points={linePoints(segment)}
                  fill="none"
                  stroke={stroke}
                  strokeWidth={1.5}
                  strokeLinejoin="round"
                  strokeLinecap="round"
                  vectorEffect="non-scaling-stroke"
                />
              </g>
            ))}
            {/*
              A zero-length stroke with round caps, not a <circle>.

              The viewBox is 300 wide and preserveAspectRatio="none" stretches
              it horizontally; vectorEffect protects strokes, not fill
              geometry, so an r=3 circle became a 9x3 ELLIPSE on a 900 px card
              and changed shape as the side panel was resized. A stroke is
              exempt from the stretch, so this dot stays a dot.
            */}
            {last === null ? null : (
              <path
                d={`M${round(last.x)} ${round(last.y)}h0`}
                stroke={stroke}
                strokeWidth={6}
                strokeLinecap="round"
                vectorEffect="non-scaling-stroke"
              />
            )}
          </g>
        );
      })}
    </svg>
  );

  if (ticks === undefined || ticks.length === 0) {
    return <div className={classes}>{chart}</div>;
  }

  return (
    <div className={classes}>
      {chart}
      {/*
        aria-hidden: the marks are a reading aid for the curve, and the curve
        is already summarised by its own label. Announcing three bare times
        after it would say nothing a screen reader could use.
      */}
      <div
        aria-hidden
        className="mt-1 flex justify-between text-[10px] text-text-muted tabular-nums"
      >
        {ticks.map((tick, index) => (
          <span key={`${tick}:${String(index)}`}>{tick}</span>
        ))}
      </div>
    </div>
  );
}

/**
 * Turns the values into runs of consecutive known ones.
 *
 * Every gap starts a new run, so the line breaks where the data does rather
 * than drawing a straight segment across an outage.
 */
function buildSegments(
  values: (number | null)[],
  scaleMax: number,
  height: number,
): Sample[][] {
  if (values.length === 0) {
    return [];
  }

  const top = Number.isFinite(scaleMax) && scaleMax > 0 ? scaleMax : 1;
  const step = values.length > 1 ? VIEW_WIDTH / (values.length - 1) : 0;

  const segments: Sample[][] = [];
  let current: Sample[] = [];

  values.forEach((value, index) => {
    if (value === null || !Number.isFinite(value)) {
      if (current.length > 0) {
        segments.push(current);
        current = [];
      }
      return;
    }
    const ratio = Math.min(1, Math.max(0, value / top));
    current.push({
      x: values.length > 1 ? index * step : VIEW_WIDTH / 2,
      // SVG y grows downwards, so a full ratio sits at the top.
      y: height - ratio * height,
    });
  });

  if (current.length > 0) {
    segments.push(current);
  }
  return segments;
}

function linePoints(segment: Sample[]): string {
  return segment.map((s) => `${round(s.x)},${round(s.y)}`).join(" ");
}

/** Closes a run down to the baseline to make the filled area under the line. */
function areaPoints(segment: Sample[], height: number): string {
  const first = segment[0];
  const last = segment[segment.length - 1];
  if (first === undefined || last === undefined) {
    return "";
  }
  return [
    `${round(first.x)},${String(height)}`,
    linePoints(segment),
    `${round(last.x)},${String(height)}`,
  ].join(" ");
}

function lastSample(segments: Sample[][]): Sample | null {
  const lastSegment = segments[segments.length - 1];
  if (lastSegment === undefined) {
    return null;
  }
  return lastSegment[lastSegment.length - 1] ?? null;
}

function round(value: number): string {
  return (Math.round(value * 100) / 100).toString();
}
