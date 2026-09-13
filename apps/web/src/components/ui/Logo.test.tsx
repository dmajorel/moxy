import { render, screen } from "@testing-library/react";

import { Logo } from "./Logo";

describe("Logo", () => {
  it("draws the mark at 22px in the brand colour", () => {
    const { container } = render(<Logo />);

    const svg = container.firstElementChild;
    expect(svg?.tagName).toBe("svg");
    expect(svg).toHaveClass("text-brand");
    expect(svg).toHaveAttribute("width", "22");
    expect(svg).toHaveAttribute("height", "22");
    expect(svg).toHaveAttribute("viewBox", "0 0 32 32");
  });

  it("fills from currentColor so the token decides the colour", () => {
    const { container } = render(<Logo />);

    expect(container.firstElementChild).toHaveAttribute("fill", "currentColor");
  });

  it("draws the four arms as one path", () => {
    const { container } = render(<Logo />);

    const paths = container.querySelectorAll("path");
    expect(paths).toHaveLength(1);
    // Four subpaths, one per arm; the gaps between them are holes, not strokes.
    expect(paths.item(0)?.getAttribute("d")?.match(/M/g)).toHaveLength(4);
  });

  it("hides itself from assistive technology when it carries no label", () => {
    const { container } = render(<Logo />);

    const svg = container.firstElementChild;
    expect(svg).toHaveAttribute("aria-hidden", "true");
    expect(svg).not.toHaveAttribute("role");
  });

  it("becomes an image with a name when a title is given", () => {
    render(<Logo title="moxy" />);

    const svg = screen.getByRole("img", { name: "moxy" });
    expect(svg).not.toHaveAttribute("aria-hidden");
  });

  it("accepts a size and merges the className it receives", () => {
    const { container } = render(<Logo size={40} className="mr-2" />);

    const svg = container.firstElementChild;
    expect(svg).toHaveAttribute("width", "40");
    expect(svg).toHaveClass("mr-2");
    expect(svg).toHaveClass("text-brand");
  });
});
