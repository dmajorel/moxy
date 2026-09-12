import { render, screen } from "@testing-library/react";

import { StatusDot } from "./StatusDot";
import type { StatusDotStatus } from "./StatusDot";

describe("StatusDot", () => {
  it.each<[StatusDotStatus, string]>([
    ["online", "bg-success"],
    ["running", "bg-success"],
    ["healthy", "bg-success"],
    ["maintenance", "bg-warning"],
    ["degraded", "bg-warning"],
    ["offline", "bg-text-muted"],
    ["stopped", "bg-text-muted"],
    ["unknown", "bg-text-muted"],
    ["unreachable", "bg-text-muted"],
    ["template", "bg-text-muted"],
  ])("paints %s with the %s token", (status, expected) => {
    const { container } = render(<StatusDot status={status} />);

    const dot = container.firstElementChild;
    expect(dot).toHaveClass(expected);
    expect(dot).toHaveClass("size-[7px]");
    expect(dot).toHaveClass("rounded-full");
  });

  it("exposes a default alternative text for a status", () => {
    render(<StatusDot status="maintenance" />);

    expect(screen.getByRole("img", { name: "En maintenance" })).toBeInTheDocument();
  });

  it("lets the caller override the alternative text", () => {
    render(<StatusDot status="online" title="En ligne · 2 j 22 h" />);

    const dot = screen.getByRole("img", { name: "En ligne · 2 j 22 h" });
    expect(dot).toHaveAttribute("title", "En ligne · 2 j 22 h");
  });

  it("merges the className it receives with its own classes", () => {
    const { container } = render(<StatusDot status="online" className="mr-2" />);

    const dot = container.firstElementChild;
    expect(dot).toHaveClass("mr-2");
    expect(dot).toHaveClass("bg-success");
  });
});
