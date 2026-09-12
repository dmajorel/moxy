import { describe, expect, it } from "vitest";

import type { Point } from "@/api/types";

import { cpuRatios, memoryRatios } from "./series";

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
