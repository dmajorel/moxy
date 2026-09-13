import { describe, expect, it } from "vitest";

import type { Point } from "@/api/types";

import { cpuRatios, memoryRatios, timeTicks } from "./series";

function point(values: Partial<Point>): Point {
  return {
    time: "2026-09-12T10:00:00Z",
    cpu: null,
    memUsed: null,
    memTotal: null,
    netIn: null,
    netOut: null,
    ...values,
  };
}

describe("cpuRatios", () => {
  it("reads the fraction of every sample and keeps the gaps", () => {
    const values = cpuRatios([
      point({ cpu: 0.31 }),
      point({ cpu: null }),
      point({ cpu: 0 }),
    ]);

    // The hole stays a hole: a zero there would draw a drop that never
    // happened, and a cluster at rest is not a cluster nobody could measure.
    expect(values).toEqual([0.31, null, 0]);
  });

  it("refuses a value that is not a number", () => {
    expect(cpuRatios([point({ cpu: Number.NaN })])).toEqual([null]);
  });
});

describe("memoryRatios", () => {
  it("divides used by total", () => {
    expect(memoryRatios([point({ memUsed: 2, memTotal: 8 })])).toEqual([0.25]);
  });

  it("says nothing when either side is missing", () => {
    // memUsed alone says nothing about a share, and a total of zero is a node
    // that reported no figures rather than a node without memory.
    const values = memoryRatios([
      point({ memUsed: 2, memTotal: null }),
      point({ memUsed: null, memTotal: 8 }),
      point({ memUsed: 2, memTotal: 0 }),
    ]);

    expect(values).toEqual([null, null, null]);
  });
});

describe("timeTicks", () => {
  /** An hour of samples, one a minute, the way an RRD window arrives. */
  function hour(): Point[] {
    return Array.from({ length: 61 }, (_, index) =>
      point({
        time: new Date(2026, 8, 12, 11, index).toISOString(),
        cpu: 0.1,
      }),
    );
  }

  // The three marks of appendix A.1, and the middle one is the MEDIAN SAMPLE
  // rather than the midpoint of the two times: the marks line up with the
  // points actually drawn, so a window with a hole in it is never labelled
  // with an instant it does not contain.
  it("marks the start, the middle and the end of the window", () => {
    expect(timeTicks(hour(), "hour")).toEqual(["11:00", "11:30", "12:00"]);
  });

  it("writes no seconds", () => {
    const ticks = timeTicks(hour(), "hour");
    for (const tick of ticks) {
      expect(tick).toMatch(/^\d{2}:\d{2}$/);
    }
  });

  // A clock says nothing over a month: the marks have to name the day, or the
  // three of them read as three instants nobody can place.
  it("names the day past the day, and the month past the month", () => {
    const spread = (days: number): Point[] =>
      Array.from({ length: 3 }, (_, index) =>
        point({
          time: new Date(2026, 8, 1 + index * days, 11, 0).toISOString(),
          cpu: 0.1,
        }),
      );

    expect(timeTicks(spread(3), "week")).toEqual(["01/09", "04/09", "07/09"]);
    expect(timeTicks(spread(15), "month")).toEqual(["01/09", "16/09", "01/10"]);
    expect(timeTicks(spread(120), "year")).toEqual(["09/2026", "12/2026", "04/2027"]);
  });

  // Fewer marks rather than a repeated one: two identical labels under a chart
  // say the window has no width, which is not what a short window means.
  it("degrades on a window too short for three", () => {
    expect(timeTicks([], "hour")).toEqual([]);
    expect(
      timeTicks([point({ time: "2026-09-12T11:00:00Z" })], "hour"),
    ).toHaveLength(1);
    expect(
      timeTicks(
        [
          point({ time: "2026-09-12T11:00:00Z" }),
          point({ time: "2026-09-12T11:30:00Z" }),
        ],
        "hour",
      ),
    ).toHaveLength(2);
  });

  // A gap is still a sample: it has a time, and dropping it would shift every
  // mark away from the curve it is meant to label.
  it("counts a gap as a sample", () => {
    const points = [
      point({ time: new Date(2026, 8, 12, 11, 0).toISOString(), cpu: 0.1 }),
      point({ time: new Date(2026, 8, 12, 11, 30).toISOString() }),
      point({ time: new Date(2026, 8, 12, 12, 0).toISOString(), cpu: 0.2 }),
    ];

    expect(timeTicks(points, "hour")).toEqual(["11:00", "11:30", "12:00"]);
  });
});
