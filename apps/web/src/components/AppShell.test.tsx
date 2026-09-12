import { render, screen } from "@testing-library/react";

import { AppShell } from "./AppShell";

function renderShell() {
  return render(
    <AppShell topBar={<span>top bar region</span>} sidebar={<span>sidebar region</span>}>
      <span>content region</span>
    </AppShell>,
  );
}

describe("AppShell", () => {
  it("places its three regions", () => {
    renderShell();

    expect(screen.getByText("top bar region")).toBeInTheDocument();
    expect(screen.getByText("sidebar region")).toBeInTheDocument();
    expect(screen.getByText("content region")).toBeInTheDocument();
  });

  it("exposes the two columns as aside and main", () => {
    const { container } = renderShell();

    const aside = container.querySelector("aside");
    const main = container.querySelector("main");
    expect(aside).not.toBeNull();
    expect(main).not.toBeNull();
    expect(aside).toContainElement(screen.getByText("sidebar region"));
    expect(main).toContainElement(screen.getByText("content region"));
    // The top bar sits outside both columns, so it never scrolls with them.
    expect(aside).not.toContainElement(screen.getByText("top bar region"));
    expect(main).not.toContainElement(screen.getByText("top bar region"));
  });

  it("gives the sidebar the fixed 190px column of appendix A.2", () => {
    const { container } = renderShell();

    const grid = container.querySelector("aside")?.parentElement;
    expect(grid).toHaveClass("grid-cols-[190px_minmax(0,1fr)]");
  });

  it("lets each column scroll on its own", () => {
    const { container } = renderShell();

    expect(container.querySelector("aside")).toHaveClass("overflow-y-auto");
    expect(container.querySelector("main")).toHaveClass("overflow-y-auto");
  });

  it("offers a skip link that targets the main region", () => {
    const { container } = renderShell();

    const link = screen.getByRole("link", { name: "Aller au contenu" });
    const main = container.querySelector("main");
    expect(link).toHaveAttribute("href", `#${main?.id ?? ""}`);
    expect(main?.id).toBeTruthy();
    // Focusable by script so the jump really moves keyboard focus.
    expect(main).toHaveAttribute("tabindex", "-1");
  });

  it("keeps the skip link out of sight until it takes focus", () => {
    renderShell();

    const link = screen.getByRole("link", { name: "Aller au contenu" });
    expect(link).toHaveClass("sr-only");
    expect(link).toHaveClass("focus:not-sr-only");
  });

  it("merges the className it receives with its own classes", () => {
    const { container } = render(
      <AppShell className="border-t-[0.5px]" topBar={null} sidebar={null}>
        {null}
      </AppShell>,
    );

    const root = container.firstElementChild;
    expect(root).toHaveClass("border-t-[0.5px]");
    expect(root).toHaveClass("h-screen");
  });
});
