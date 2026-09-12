import { render, screen } from "@testing-library/react";

import { AlertBanner } from "./AlertBanner";

describe("AlertBanner", () => {
  it("renders the neutral variant with a check icon by default", () => {
    const { container } = render(<AlertBanner>Quorum 3/3 · aucune alerte</AlertBanner>);

    const banner = container.firstElementChild;
    expect(banner).toHaveClass("bg-surface-1");
    expect(banner).toHaveClass("text-text-secondary");
    expect(container.querySelector(".tabler-icon-check")).not.toBeNull();
    expect(screen.getByText("Quorum 3/3 · aucune alerte")).toBeInTheDocument();
  });

  it("renders the warning variant with an alert icon by default", () => {
    const { container } = render(
      <AlertBanner variant="warning">Mémoire à 83 % sur 2 nœuds</AlertBanner>,
    );

    const banner = container.firstElementChild;
    expect(banner).toHaveClass("bg-bg-warning");
    expect(banner).toHaveClass("text-text-warning");
    expect(container.querySelector(".tabler-icon-alert-triangle")).not.toBeNull();
  });

  it("lets the caller pick the icon", () => {
    const { container } = render(
      <AlertBanner variant="warning" icon="refresh">
        Mise à jour 9.2.12 disponible sur 5 nœuds
      </AlertBanner>,
    );

    expect(container.querySelector(".tabler-icon-refresh")).not.toBeNull();
    expect(container.querySelector(".tabler-icon-alert-triangle")).toBeNull();
  });

  it("hides its icon from assistive technology", () => {
    const { container } = render(<AlertBanner>Aucune alerte</AlertBanner>);

    expect(container.querySelector("svg")).toHaveAttribute("aria-hidden", "true");
  });

  it("merges the className it receives with its own classes", () => {
    const { container } = render(<AlertBanner className="mt-2.5">Aucune alerte</AlertBanner>);

    const banner = container.firstElementChild;
    expect(banner).toHaveClass("mt-2.5");
    expect(banner).toHaveClass("rounded-card");
  });
});
