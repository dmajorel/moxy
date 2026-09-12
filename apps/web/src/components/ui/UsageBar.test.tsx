import { render, screen } from "@testing-library/react";

import { UsageBar } from "./UsageBar";

function fillOf(container: HTMLElement): HTMLElement {
  const fill = container.querySelector("[role='progressbar'] > div");
  if (fill === null) {
    throw new Error("the bar has no fill");
  }
  return fill as HTMLElement;
}

describe("UsageBar", () => {
  it("draws the fill at the given ratio", () => {
    const { container } = render(<UsageBar ratio={0.16} />);

    expect(fillOf(container)).toHaveStyle({ width: "16%" });
  });

  it.each([
    [-0.5, "0%"],
    [1.5, "100%"],
    [Number.NaN, "0%"],
    [Number.POSITIVE_INFINITY, "0%"],
  ])("clamps the aberrant ratio %s to %s", (ratio, expected) => {
    const { container } = render(<UsageBar ratio={ratio} />);

    expect(fillOf(container)).toHaveStyle({ width: expected });
  });

  it("keeps the accent fill at the threshold itself", () => {
    const { container } = render(<UsageBar ratio={0.8} />);

    expect(fillOf(container)).toHaveClass("bg-accent");
  });

  it("turns amber above the threshold", () => {
    const { container } = render(<UsageBar ratio={0.83} />);

    expect(fillOf(container)).toHaveClass("bg-warning");
  });

  it("honours a custom threshold", () => {
    const { container } = render(<UsageBar ratio={0.6} threshold={0.5} />);

    expect(fillOf(container)).toHaveClass("bg-warning");
  });

  it("exposes the progressbar role and its bounds", () => {
    render(<UsageBar ratio={1.5} label="Mémoire" />);

    const bar = screen.getByRole("progressbar", { name: "Mémoire" });
    expect(bar).toHaveAttribute("aria-valuenow", "1");
    expect(bar).toHaveAttribute("aria-valuemin", "0");
    expect(bar).toHaveAttribute("aria-valuemax", "1");
  });

  it.each([
    [3 as const, "h-[3px]"],
    [4 as const, "h-[4px]"],
  ])("renders the %spx track", (size, expected) => {
    render(<UsageBar ratio={0.2} size={size} />);

    expect(screen.getByRole("progressbar")).toHaveClass(expected);
  });

  it("merges the className it receives with its own classes", () => {
    render(<UsageBar ratio={0.2} className="mt-2" />);

    const bar = screen.getByRole("progressbar");
    expect(bar).toHaveClass("mt-2");
    expect(bar).toHaveClass("bg-surface-0");
  });
});
