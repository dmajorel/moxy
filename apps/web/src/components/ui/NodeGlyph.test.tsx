import { render, screen } from "@testing-library/react";

import type { NodeStatus } from "@/api/types";
import { NodeGlyph } from "./NodeGlyph";

describe("NodeGlyph", () => {
  it.each<[NodeStatus, string]>([
    ["online", "text-text-success"],
    ["maintenance", "text-text-warning-strong"],
    ["offline", "text-text-muted"],
    ["unknown", "text-text-muted"],
  ])("paints %s with the %s token", (status, expected) => {
    const { container } = render(<NodeGlyph status={status} />);

    expect(container.firstElementChild).toHaveClass(expected);
  });

  // The wrench REPLACES the server rather than joining it: a badge has no room
  // to exist at 13px, and two marks for one fact are read twice.
  it("swaps the server for the wrench while the node is drained", () => {
    const { container: drained } = render(<NodeGlyph status="maintenance" />);
    expect(drained.firstElementChild).toHaveClass("tabler-icon-tool");

    const { container: online } = render(<NodeGlyph status="online" />);
    expect(online.firstElementChild).toHaveClass("tabler-icon-server");
  });

  // Nothing is carried by the colour alone, and one fact is named once.
  it.each<[NodeStatus, string]>([
    ["online", "En ligne"],
    ["maintenance", "Maintenance"],
    ["offline", "Hors ligne"],
    ["unknown", "Inconnu"],
  ])("names %s in words", (status, expected) => {
    const { container } = render(<NodeGlyph status={status} />);

    expect(screen.getByRole("img", { name: expected })).toBeInTheDocument();
    // The same word under the pointer as in the accessibility tree. Tabler
    // turns `title` into a <title> child of the svg, not into an attribute,
    // which is what draws the tooltip.
    expect(container.querySelector("svg > title")?.textContent).toBe(expected);
    expect(screen.getAllByRole("img")).toHaveLength(1);
  });

  it("takes an extra class without losing its own", () => {
    const { container } = render(<NodeGlyph status="online" className="mt-1" />);

    expect(container.firstElementChild).toHaveClass("mt-1");
    expect(container.firstElementChild).toHaveClass("text-text-success");
    expect(container.firstElementChild).toHaveClass("flex-none");
  });
});
