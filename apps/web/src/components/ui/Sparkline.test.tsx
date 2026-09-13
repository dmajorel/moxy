import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { Sparkline } from "./Sparkline";

/** One curve of ratios, which is all the chart knows about a metric. */
function curve(...values: (number | null)[]) {
  return [{ values }];
}

function polylines(container: HTMLElement): string[] {
  return Array.from(container.querySelectorAll("polyline")).map(
    (node) => node.getAttribute("points") ?? "",
  );
}

function strokes(container: HTMLElement): (string | null)[] {
  return Array.from(container.querySelectorAll("polyline")).map((node) =>
    node.getAttribute("stroke"),
  );
}

describe("Sparkline", () => {
  it("pins the vertical axis instead of fitting it to the data", () => {
    // The whole point of section 2: a node idling at 0.6% must read as idle,
    // not as a mountain range. Both series below are tiny, so both must draw
    // near the bottom of a 70px box — an auto-scaled chart would put each
    // series' own maximum at the top and make them look identical.
    const tiny = render(<Sparkline series={curve(0.004, 0.006)} label="a" />);
    const tinyY = yValues(polylines(tiny.container)[0] ?? "");
    tiny.unmount();

    const bigger = render(<Sparkline series={curve(0.4, 0.6)} label="b" />);
    const biggerY = yValues(polylines(bigger.container)[0] ?? "");

    expect(Math.min(...tinyY)).toBeGreaterThan(65);
    expect(Math.min(...biggerY)).toBeLessThan(45);
  });

  it("puts a full ratio at the top and a zero at the bottom", () => {
    const { container } = render(
      <Sparkline series={curve(0, 1)} label="charge" height={70} />,
    );

    const ys = yValues(polylines(container)[0] ?? "");
    expect(ys[0]).toBeCloseTo(70, 5);
    expect(ys[1]).toBeCloseTo(0, 5);
  });

  it("breaks the line on a gap rather than drawing through it", () => {
    // RRD returns null for missing samples. Treating them as zero would draw
    // a drop that never happened.
    const { container } = render(
      <Sparkline series={curve(0.5, 0.5, null, 0.5)} label="charge" />,
    );

    expect(polylines(container)).toHaveLength(2);
  });

  it("clamps a ratio beyond the scale instead of overflowing the box", () => {
    const { container } = render(<Sparkline series={curve(4, 9)} label="x" />);

    for (const y of yValues(polylines(container)[0] ?? "")) {
      expect(y).toBeGreaterThanOrEqual(0);
    }
  });

  it("honours a raised ceiling without reading it from the data", () => {
    const { container } = render(
      <Sparkline series={curve(0.5, 0.5)} scaleMax={2} height={70} label="x" />,
    );

    // 0.5 of a ceiling of 2 is a quarter of the way up: y = 70 - 17.5.
    expect(yValues(polylines(container)[0] ?? "")[0]).toBeCloseTo(52.5, 5);
  });

  it("draws a second curve alongside the first, on the same scale", () => {
    const { container } = render(
      <Sparkline
        series={[
          { values: [0, 1], tone: "primary" },
          { values: [0, 1], tone: "secondary" },
        ]}
        height={70}
        label="Utilisation"
      />,
    );

    const lines = polylines(container);
    expect(lines).toHaveLength(2);
    // Same values, same geometry: the second curve is not rescaled to itself.
    expect(lines[0]).toBe(lines[1]);
    // Told apart by tone, and both tones are tokens rather than literals.
    expect(strokes(container)).toEqual(["var(--accent)", "var(--text-muted)"]);
  });

  it("fills the area under the primary curve only", () => {
    const { container } = render(
      <Sparkline
        series={[
          { values: [0.2, 0.4], tone: "primary" },
          { values: [0.2, 0.4], tone: "secondary" },
        ]}
        label="Utilisation"
      />,
    );

    expect(container.querySelectorAll("polygon")).toHaveLength(1);
  });

  it("says so when there is nothing to draw", () => {
    render(<Sparkline series={[]} label="Charge CPU" />);

    expect(screen.getByRole("img", { name: /aucune donnée/i })).toBeInTheDocument();
  });

  it("draws nothing but keeps its height when every sample is a gap", () => {
    render(<Sparkline series={curve(null, null)} label="Charge CPU" />);

    expect(screen.getByRole("img", { name: /aucune donnée/i })).toBeInTheDocument();
  });

  it("exposes its label to assistive technology", () => {
    render(<Sparkline series={curve(0.1, 0.2)} label="Charge CPU · moy. 0,42 %" />);

    expect(
      screen.getByRole("img", { name: "Charge CPU · moy. 0,42 %" }),
    ).toBeInTheDocument();
  });

  // The viewBox is 300 wide and preserveAspectRatio="none" stretches it;
  // vectorEffect protects strokes, not fill geometry, so an r=3 <circle> was a
  // 9x3 ellipse on a 900 px card and changed shape as the panel was resized.
  it("marks the last point with a stroke, never a circle", () => {
    const { container } = render(<Sparkline series={curve(0.1, 0.2, 0.3)} label="a" />);

    expect(container.querySelector("circle")).toBeNull();

    const marker = container.querySelector("path");
    expect(marker).not.toBeNull();
    // A zero-length segment with round caps: a dot that cannot be stretched.
    expect(marker?.getAttribute("d")).toMatch(/h0$/);
    expect(marker).toHaveAttribute("stroke-linecap", "round");
    expect(marker).toHaveAttribute("vector-effect", "non-scaling-stroke");
  });

  it("puts the marker on the last known sample, not past a trailing gap", () => {
    const { container } = render(
      <Sparkline series={curve(0.1, 0.9, null)} label="a" height={100} />,
    );

    const d = container.querySelector("path")?.getAttribute("d") ?? "";
    // 0.9 of a 100px box sits 10 from the top.
    expect(d).toContain("10h0");
  });

  // A chart with no time axis does not say WHEN the spike it shows happened.
  it("writes the time marks under the curve when it is given some", () => {
    render(
      <Sparkline
        series={curve(0.1, 0.2)}
        label="a"
        ticks={["11:00", "11:30", "12:00"]}
      />,
    );

    expect(screen.getByText("11:00")).toBeInTheDocument();
    expect(screen.getByText("11:30")).toBeInTheDocument();
    expect(screen.getByText("12:00")).toBeInTheDocument();
  });

  it("draws no axis row when it is given none", () => {
    const { container } = render(<Sparkline series={curve(0.1, 0.2)} label="a" />);

    expect(container.textContent).toBe("");
  });
});

function yValues(points: string): number[] {
  return points
    .split(" ")
    .filter((pair) => pair !== "")
    .map((pair) => Number(pair.split(",")[1]));
}
