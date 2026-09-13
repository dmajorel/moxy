import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import type { Point, Series } from "@/api/types";

import { ChartCard } from "./ChartCard";

function point(time: string, cpu: number | null): Point {
  return { time, cpu, memUsed: null, memTotal: null, netIn: null, netOut: null };
}

function series(overrides: Partial<Series> = {}): Series {
  return {
    cluster: "qual",
    timeframe: "hour",
    fetchedAt: "2026-09-12T12:00:00Z",
    cpuAverage: 0.42,
    points: [
      point("2026-09-12T11:00:00Z", 0.1),
      // A gap, which the chart must cut rather than draw at zero.
      point("2026-09-12T11:30:00Z", null),
      point("2026-09-12T12:00:00Z", 0.3),
    ],
    ...overrides,
  };
}

describe("ChartCard", () => {
  it("names the window and its average", () => {
    render(<ChartCard title="Charge CPU" label="Charge CPU de pve-01" series={series()} />);

    expect(screen.getByRole("heading", { name: "Charge CPU" })).toBeInTheDocument();
    expect(screen.getByText(/Dernière heure · moy\. 42/)).toBeInTheDocument();
    expect(screen.getByRole("img", { name: "Charge CPU de pve-01" })).toBeInTheDocument();
  });

  // An average nobody could read is not an average of zero: the window is
  // still named, and the figure simply is not there.
  it("names the window alone when the series could not be read", () => {
    render(<ChartCard title="Charge CPU" label="Charge CPU de pve-01" series={null} />);

    expect(screen.getByText("Dernière heure")).toBeInTheDocument();
    expect(screen.queryByText(/moy\./)).toBeNull();
  });

  it("writes the time marks of the samples it draws", () => {
    const { container } = render(
      <ChartCard title="Charge CPU" label="Charge CPU de vm-103" series={series()} />,
    );

    // Three marks, as appendix A.1 draws them: start, middle, end.
    expect(container.textContent).toMatch(/\d{2}:\d{2}/);
  });

  it("merges the className it receives", () => {
    const { container } = render(
      <ChartCard title="Charge CPU" label="…" series={null} className="mb-4" />,
    );

    expect(container.firstElementChild).toHaveClass("mb-4");
  });
});
