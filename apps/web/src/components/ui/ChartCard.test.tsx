import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import type { Series } from "@/api/types";

import { ChartCard } from "./ChartCard";

function series(patch: Partial<Series> = {}): Series {
  return {
    timeframe: "hour",
    cpuAverage: 0.0412,
    points: [
      { time: "2026-09-12T11:00:00Z", cpu: 0.04, memory: null },
      { time: "2026-09-12T11:30:00Z", cpu: null, memory: null },
      { time: "2026-09-12T12:00:00Z", cpu: 0.06, memory: null },
    ],
    ...patch,
  } as unknown as Series;
}

describe("ChartCard", () => {
  it("names the card and the chart separately", () => {
    render(
      <ChartCard
        title="Charge CPU du nœud"
        label="Charge CPU de prox-qual-2201-cit"
        series={series()}
      />,
    );

    expect(
      screen.getByRole("heading", { name: "Charge CPU du nœud" }),
    ).toBeInTheDocument();
    // The heading says what; the chart's own name says of what, which is the
    // only thing a screen reader hears when it lands on the figure.
    expect(
      screen.getByRole("img", { name: "Charge CPU de prox-qual-2201-cit" }),
    ).toBeInTheDocument();
  });

  // The figure goes through lib/format like every other one: a second rounding
  // written here would disagree with the metric card above it.
  it("writes the average through the one formatter", () => {
    render(<ChartCard title="Charge CPU" label="Charge CPU de x" series={series()} />);

    expect(screen.getByText("Dernière heure · moy. 4,1 %")).toBeInTheDocument();
  });

  // A legend reading "moy. —" spends the line saying that the chart above it is
  // empty, which the empty chart already says.
  it("drops the average when the hour could not be read", () => {
    render(<ChartCard title="Charge CPU" label="Charge CPU de x" series={null} />);

    expect(screen.getByText("Dernière heure")).toBeInTheDocument();
    expect(screen.queryByText(/moy\./)).toBeNull();
    // The chart keeps its place and says it has nothing, so the card keeps
    // its shape instead of collapsing when a series fails.
    expect(
      screen.getByRole("img", { name: "Charge CPU de x — aucune donnée" }),
    ).toBeInTheDocument();
  });

  // The "11:00 · 11:30 · 12:00" of appendix A.1: a chart with no time axis does
  // not say when the spike it shows happened.
  it("carries the time marks of its points", () => {
    const { container } = render(
      <ChartCard title="Charge CPU" label="Charge CPU de x" series={series()} />,
    );

    expect(container.textContent).toMatch(/\d{2}:\d{2}/);
  });

  it("draws no marks when there is nothing to mark", () => {
    const { container } = render(
      <ChartCard title="Charge CPU" label="Charge CPU de x" series={null} />,
    );

    expect(container.textContent).not.toMatch(/\d{2}:\d{2}/);
  });

  it("merges the className it receives", () => {
    const { container } = render(
      <ChartCard
        title="Charge CPU"
        label="Charge CPU de x"
        series={null}
        className="mb-3"
      />,
    );

    expect(container.querySelector("section")).toHaveClass("mb-3");
    expect(container.querySelector("section")).toHaveClass("bg-surface-2");
  });
});
