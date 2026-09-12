import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import type { Point } from "@/api/types";

import { Sparkline } from "./Sparkline";

function point(cpu: number | null, minute = 0): Point {
  return {
    time: `2026-09-12T10:${String(minute).padStart(2, "0")}:00Z`,
    cpu,
    memUsed: null,
    memTotal: null,
    netIn: null,
    netOut: null,
  };
}

function polylines(container: HTMLElement): string[] {
  return Array.from(container.querySelectorAll("polyline")).map(
    (node) => node.getAttribute("points") ?? "",
  );
}

describe("Sparkline", () => {
  it("pins the vertical axis instead of fitting it to the data", () => {
    // The whole point of section 2: a node idling at 0.6% must read as idle,
    // not as a mountain range. Both series below are tiny, so both must draw
    // near the bottom of a 70px box — an auto-scaled chart would put each
    // series' own maximum at the top and make them look identical.
    const tiny = render(<Sparkline points={[point(0.004), point(0.006, 1)]} label="a" />);
    const tinyY = yValues(polylines(tiny.container)[0] ?? "");
    tiny.unmount();

    const bigger = render(<Sparkline points={[point(0.4), point(0.6, 1)]} label="b" />);
    const biggerY = yValues(polylines(bigger.container)[0] ?? "");

    expect(Math.min(...tinyY)).toBeGreaterThan(65);
    expect(Math.min(...biggerY)).toBeLessThan(45);
  });

  it("puts a full ratio at the top and a zero at the bottom", () => {
    const { container } = render(
      <Sparkline points={[point(0), point(1, 1)]} label="charge" height={70} />,
    );

    const ys = yValues(polylines(container)[0] ?? "");
    expect(ys[0]).toBeCloseTo(70, 5);
    expect(ys[1]).toBeCloseTo(0, 5);
  });

  it("breaks the line on a gap rather than drawing through it", () => {
    // RRD returns null for missing samples. Treating them as zero would draw
    // a drop that never happened.
    const { container } = render(
      <Sparkline
        points={[point(0.5), point(0.5, 1), point(null, 2), point(0.5, 3)]}
        label="charge"
      />,
    );

    expect(polylines(container)).toHaveLength(2);
  });

  it("clamps a ratio beyond the scale instead of overflowing the box", () => {
    const { container } = render(<Sparkline points={[point(4), point(9, 1)]} label="x" />);

    for (const y of yValues(polylines(container)[0] ?? "")) {
      expect(y).toBeGreaterThanOrEqual(0);
    }
  });

  it("honours a raised ceiling without reading it from the data", () => {
    const { container } = render(
      <Sparkline points={[point(0.5), point(0.5, 1)]} scaleMax={2} height={70} label="x" />,
    );

    // 0.5 of a ceiling of 2 is a quarter of the way up: y = 70 - 17.5.
    expect(yValues(polylines(container)[0] ?? "")[0]).toBeCloseTo(52.5, 5);
  });

  it("says so when there is nothing to draw", () => {
    render(<Sparkline points={[]} label="Charge CPU" />);

    expect(screen.getByRole("img", { name: /aucune donnée/i })).toBeInTheDocument();
  });

  it("draws nothing but keeps its height when every sample is a gap", () => {
    render(<Sparkline points={[point(null), point(null, 1)]} label="Charge CPU" />);

    expect(screen.getByRole("img", { name: /aucune donnée/i })).toBeInTheDocument();
  });

  it("exposes its label to assistive technology", () => {
    render(<Sparkline points={[point(0.1), point(0.2, 1)]} label="Charge CPU · moy. 0,42 %" />);

    expect(
      screen.getByRole("img", { name: "Charge CPU · moy. 0,42 %" }),
    ).toBeInTheDocument();
  });
});

function yValues(points: string): number[] {
  return points
    .split(" ")
    .filter((pair) => pair !== "")
    .map((pair) => Number(pair.split(",")[1]));
}
