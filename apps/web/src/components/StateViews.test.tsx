import { fireEvent, render, screen } from "@testing-library/react";

import { EmptyView, ErrorView, LoadingView, StaleBanner } from "./StateViews";

/** The English message the backend would send; it is diagnostic material only. */
const BACKEND_MESSAGE = "method not allowed";

describe("LoadingView", () => {
  it("announces the first load in French, without a spinner", () => {
    const { container } = render(<LoadingView />);

    expect(screen.getByRole("status")).toHaveTextContent("Chargement…");
    expect(container.querySelector("svg")).toBeNull();
  });
});

describe("ErrorView", () => {
  it("explains the failure in French", () => {
    render(<ErrorView error={new Error(BACKEND_MESSAGE)} />);

    expect(
      screen.getByRole("heading", { name: "Impossible de charger les données" }),
    ).toBeInTheDocument();
  });

  it("keeps the backend's English message inside the collapsed details", () => {
    const { container } = render(<ErrorView error={new Error(BACKEND_MESSAGE)} />);

    const details = container.querySelector("details");
    expect(details).not.toBeNull();
    expect(details).not.toHaveAttribute("open");
    expect(screen.getByText("Détail technique")).toBeInTheDocument();

    const message = screen.getByText(BACKEND_MESSAGE);
    expect(details).toContainElement(message);
  });

  it("offers a retry only when it can retry", () => {
    const onRetry = vi.fn();
    const { rerender } = render(<ErrorView error={new Error(BACKEND_MESSAGE)} />);
    expect(screen.queryByRole("button", { name: "Réessayer" })).toBeNull();

    rerender(<ErrorView error={new Error(BACKEND_MESSAGE)} onRetry={onRetry} />);
    fireEvent.click(screen.getByRole("button", { name: "Réessayer" }));
    expect(onRetry).toHaveBeenCalledTimes(1);
  });
});

describe("StaleBanner", () => {
  it("dates the reading still on screen", () => {
    render(<StaleBanner lastUpdatedAt={new Date(2026, 8, 12, 14, 32)} />);

    expect(
      screen.getByText("Données du 12/09/2026 à 14:32 · connexion perdue"),
    ).toBeInTheDocument();
  });

  it("falls back to a dateless sentence when no timestamp was recorded", () => {
    render(<StaleBanner lastUpdatedAt={null} />);

    expect(screen.getByText("Données précédentes · connexion perdue")).toBeInTheDocument();
  });

  it("uses the warning variant of AlertBanner", () => {
    const { container } = render(<StaleBanner lastUpdatedAt={null} />);

    expect(container.firstElementChild).toHaveClass("bg-bg-warning");
    expect(container.querySelector(".tabler-icon-alert-triangle")).not.toBeNull();
  });

  it("calls onRetry when offered", () => {
    const onRetry = vi.fn();
    render(<StaleBanner lastUpdatedAt={new Date(2026, 8, 12, 14, 32)} onRetry={onRetry} />);

    fireEvent.click(screen.getByRole("button", { name: "Réessayer" }));
    expect(onRetry).toHaveBeenCalledTimes(1);
  });
});

describe("EmptyView", () => {
  it("states the title and the optional hint", () => {
    const { rerender } = render(<EmptyView title="Aucun cluster à afficher" />);
    expect(screen.getByText("Aucun cluster à afficher")).toBeInTheDocument();
    expect(screen.queryByText("Ajoutez un cluster.")).toBeNull();

    rerender(<EmptyView title="Aucun cluster à afficher" hint="Ajoutez un cluster." />);
    expect(screen.getByText("Ajoutez un cluster.")).toBeInTheDocument();
  });
});
