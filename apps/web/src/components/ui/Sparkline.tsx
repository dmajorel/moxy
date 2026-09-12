import type { Point } from "@/api/types";

/**
 * Fixed-height CPU chart.
 *
 * Section 2 is categorical about this one: the vertical scale is **never**
 * auto-fitted. The native interface rescales to the data, which turns a node
 * idling at 0.6% into a dramatic mountain range and makes every node look
 * equally busy. Here the axis is pinned to [0, `scaleMax`] so that a flat line
 * means "nothing is happening" and two charts side by side are comparable.
 *
 * RRD returns gaps, and `null` means unknown. A gap is not drawn as zero — that
 * would invent a drop that never happened — it splits the line instead.
 */
export interface SparklineProps {
  points: Point[];
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
  className?: string;
}

const VIEW_WIDTH = 300;

interface Sample {
  x: number;
  y: number;
}

export function Sparkline({
  points,
  scaleMax = 1,
  height = 70,
  label,
  className,
}: SparklineProps) {
  const segments = buildSegments(points, scaleMax, height);
  const last = lastSample(segments);

  const classes = ["w-full", className].filter(Boolean).join(" ");

  if (segments.length === 0) {
    return (
      <div
        className={[classes, "flex items-center justify-center text-[11px] text-text-muted"]
          .filter(Boolean)
          .join(" ")}
        style={{ height }}
        role="img"
        aria-label={`${label} — aucune donnée`}
      >
        Aucune donnée
      </div>
    );
  }

  return (
    <svg
      className={classes}
      viewBox={`0 0 ${String(VIEW_WIDTH)} ${String(height)}`}
      height={height}
      preserveAspectRatio="none"
      role="img"
      aria-label={label}
    >
      {segments.map((segment, index) => (
        <g key={index}>
          {segment.length > 1 ? (
            <polygon
              points={areaPoints(segment, height)}
              fill="var(--bg-accent)"
            />
          ) : null}
          <polyline
            points={linePoints(segment)}
            fill="none"
            stroke="var(--accent)"
            strokeWidth={1.5}
            strokeLinejoin="round"
            strokeLinecap="round"
            vectorEffect="non-scaling-stroke"
          />
        </g>
      ))}
      {last === null ? null : (
        <circle cx={last.x} cy={last.y} r={3} fill="var(--accent)" />
      )}
    </svg>
  );
}

/**
 * Turns the samples into runs of consecutive known values.
 *
 * Every gap starts a new run, so the line breaks where the data does rather
 * than drawing a straight segment across an outage.
 */
function buildSegments(points: Point[], scaleMax: number, height: number): Sample[][] {
  if (points.length === 0) {
    return [];
  }

  const top = Number.isFinite(scaleMax) && scaleMax > 0 ? scaleMax : 1;
  const step = points.length > 1 ? VIEW_WIDTH / (points.length - 1) : 0;

  const segments: Sample[][] = [];
  let current: Sample[] = [];

  points.forEach((point, index) => {
    const value = point.cpu;
    if (value === null || !Number.isFinite(value)) {
      if (current.length > 0) {
        segments.push(current);
        current = [];
      }
      return;
    }
    const ratio = Math.min(1, Math.max(0, value / top));
    current.push({
      x: points.length > 1 ? index * step : VIEW_WIDTH / 2,
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
