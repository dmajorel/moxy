/**
 * Reading RRD points as chart values.
 *
 * A chart draws ratios; the payload carries fractions and byte counts. The
 * arithmetic between the two lives here rather than inside a component, for
 * the reason `format.ts` exists: there is one place where it is decided what a
 * value means, or two conventions appear and drift apart.
 *
 * `null` means unknown throughout, and stays unknown. A gap turned into a zero
 * would draw a drop that never happened — the very mistake the fixed scale of
 * section 2 exists to avoid.
 */
import type { Point } from "@/api/types";
import { formatClock } from "@/lib/format";

/** CPU load of each sample, as a fraction in [0,1]. Gaps are preserved. */
export function cpuRatios(points: Point[]): (number | null)[] {
  return points.map((point) => usable(point.cpu));
}

/**
 * Memory use of each sample, as a fraction of the memory the sample reported.
 *
 * Both sides must be known: a `memUsed` without its `memTotal` says nothing
 * about a share, and a total of zero is a node that reported no figures rather
 * than a node without memory.
 */
export function memoryRatios(points: Point[]): (number | null)[] {
  return points.map((point) => {
    const used = usable(point.memUsed);
    const total = usable(point.memTotal);
    if (used === null || total === null || total <= 0 || used < 0) {
      return null;
    }
    return used / total;
  });
}

/** Keeps a number only when it is one: NaN and Infinity are not measurements. */
function usable(value: number | null): number | null {
  return value !== null && Number.isFinite(value) ? value : null;
}

/**
 * The three axis marks of appendix A.1: the start of the window, its middle
 * and its end.
 *
 * A chart with no time axis does not say WHEN the spike it shows happened,
 * which is the first thing an operator asks of it. Three is what the mockup
 * draws and what fits under a chart 300 units wide without crowding.
 *
 * The middle one is the median SAMPLE, not the midpoint of the two times: the
 * marks have to line up with the points actually drawn, and a window with a
 * hole in the middle would otherwise be labelled with an instant it does not
 * contain. Fewer than three points yields fewer than three marks rather than
 * a repeated one.
 */
export function timeTicks(points: Point[]): string[] {
  const times = points
    .map((point) => point.time)
    .filter((time): time is string => typeof time === "string" && time !== "");
  if (times.length === 0) {
    return [];
  }

  const first = times[0];
  const last = times[times.length - 1];
  if (first === undefined || last === undefined) {
    return [];
  }
  if (times.length < 3) {
    return times.length === 1 ? [formatClock(first)] : [formatClock(first), formatClock(last)];
  }

  const middle = times[Math.floor((times.length - 1) / 2)];
  return [
    formatClock(first),
    middle === undefined ? FALLBACK_TICK : formatClock(middle),
    formatClock(last),
  ];
}

/** What a mark reads when its sample has no usable time. */
const FALLBACK_TICK = "—";
