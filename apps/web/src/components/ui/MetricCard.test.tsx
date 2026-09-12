import { render, screen } from "@testing-library/react";

import { MetricCard } from "./MetricCard";

describe("MetricCard", () => {
  it("renders its label, value and detail", () => {
    render(<MetricCard label="Mémoire" value="1,25" detail="/ 8 GiB" />);

    expect(screen.getByText("Mémoire")).toBeInTheDocument();
    expect(screen.getByText("1,25")).toBeInTheDocument();
    expect(screen.getByText("/ 8 GiB")).toBeInTheDocument();
  });

  it("renders no bar when no ratio is given", () => {
    render(<MetricCard label="Load average" value="0,42" />);

    expect(screen.queryByRole("progressbar")).toBeNull();
  });

  it("renders the bar when a ratio is given, named by the label", () => {
    render(<MetricCard label="Mémoire" value="1,25" ratio={0.16} />);

    expect(screen.getByRole("progressbar", { name: "Mémoire" })).toBeInTheDocument();
  });

  it("forwards the threshold to the bar", () => {
    const { container } = render(
      <MetricCard label="Mémoire" value="212" ratio={0.6} threshold={0.5} />,
    );

    expect(container.querySelector("[role='progressbar'] > div")).toHaveClass(
      "bg-warning",
    );
  });

  it("renders a hairline surface-2 card", () => {
    const { container } = render(<MetricCard label="CPU" value="4 %" />);

    const card = container.firstElementChild;
    expect(card).toHaveClass("bg-surface-2");
    expect(card).toHaveClass("border-[0.5px]");
    expect(card).toHaveClass("rounded-card");
  });

  it("merges the className it receives with its own classes", () => {
    const { container } = render(
      <MetricCard className="col-span-2" label="CPU" value="4 %" />,
    );

    const card = container.firstElementChild;
    expect(card).toHaveClass("col-span-2");
    expect(card).toHaveClass("bg-surface-2");
  });
});
