import { render, screen } from "@testing-library/react";

import { Tag } from "./Tag";

describe("Tag", () => {
  it("renders its children as a neutral pill by default", () => {
    render(<Tag>env.qualification</Tag>);

    const tag = screen.getByText("env.qualification");
    expect(tag).toHaveClass("bg-surface-2");
    expect(tag).toHaveClass("border-border");
    expect(tag).toHaveClass("rounded-full");
  });

  it.each([
    ["success" as const, "bg-bg-success", "text-text-success"],
    ["warning" as const, "bg-bg-warning", "text-text-warning"],
    ["accent" as const, "bg-bg-accent", "text-text-accent"],
  ])("renders the %s variant with its token classes", (variant, bg, fg) => {
    render(<Tag variant={variant}>Sain</Tag>);

    const tag = screen.getByText("Sain");
    expect(tag).toHaveClass(bg);
    expect(tag).toHaveClass(fg);
  });

  it("renders a leading icon when one is given", () => {
    render(<Tag icon={<svg data-testid="tag-icon" />}>2 alertes</Tag>);

    expect(screen.getByTestId("tag-icon")).toBeInTheDocument();
  });

  it("renders no icon slot when none is given", () => {
    const { container } = render(<Tag>2 alertes</Tag>);

    expect(container.querySelector("svg")).toBeNull();
  });

  it("merges the className it receives with its own classes", () => {
    render(<Tag className="ml-auto">Dégradé</Tag>);

    const tag = screen.getByText("Dégradé");
    expect(tag).toHaveClass("ml-auto");
    expect(tag).toHaveClass("text-[11px]");
  });
});
