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
