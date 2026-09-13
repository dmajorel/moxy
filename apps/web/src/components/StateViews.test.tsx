import { fireEvent, render, screen } from "@testing-library/react";

import { EmptyView, ErrorView, LoadingView, StaleBanner } from "./StateViews";
import { ApiParseError, ApiRequestError } from "@/api/client";

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

/** The route a detail screen asks for; every failure below names it. */
const NODE_PATH = "/api/clusters/prod/nodes/pve-01";

/** "moxyd is not running" is the sentence no other class may borrow. */
const OUTAGE = /Vérifiez qu’il est démarré/;

describe("ErrorView by cause", () => {
  it("tells a stopped daemon apart, and only there", () => {
    // status 0: the fetch itself failed, so nothing answered at all.
    render(<ErrorView error={new ApiRequestError(NODE_PATH, 0, null)} />);

    expect(screen.getByText("moxy est injoignable")).toBeInTheDocument();
    expect(screen.getByText(OUTAGE)).toBeInTheDocument();
  });

  it("names a vanished object, rather than accusing the daemon", () => {
    // A VM deleted between two polls: moxyd has just answered, so sending the
    // operator to check that it runs is the exact opposite of the errand.
    render(<ErrorView error={new ApiRequestError(NODE_PATH, 404, "not found")} />);

    expect(screen.getByText("Objet introuvable")).toBeInTheDocument();
    expect(screen.getByText(/n’existe plus dans le cluster/)).toBeInTheDocument();
    expect(screen.queryByText(OUTAGE)).not.toBeInTheDocument();
  });

  it("names a refusal as a privilege problem, not an outage", () => {
    // The generic wording sent an operator hunting the network for what was a
    // missing Sys.Audit on /nodes.
    render(
      <ErrorView error={new ApiRequestError(NODE_PATH, 403, "insufficient privileges")} />,
    );

    expect(screen.getByText("Droits insuffisants sur ce nœud")).toBeInTheDocument();
    expect(screen.getByText(/Sys\.Audit sur \/nodes/)).toBeInTheDocument();
    expect(screen.queryByText(OUTAGE)).not.toBeInTheDocument();
  });

  it("blames the cluster, not moxy, on a bad gateway", () => {
    render(
      <ErrorView error={new ApiRequestError(NODE_PATH, 502, "upstream unavailable")} />,
    );

    expect(screen.getByText("Cluster injoignable")).toBeInTheDocument();
    expect(screen.getByText(/le cluster PVE ne répond pas/)).toBeInTheDocument();
    expect(screen.queryByText(OUTAGE)).not.toBeInTheDocument();
  });

  it("names an upstream timeout as such", () => {
    render(
      <ErrorView error={new ApiRequestError(NODE_PATH, 504, "upstream timeout")} />,
    );

    expect(screen.getByText("Délai dépassé côté cluster")).toBeInTheDocument();
    expect(screen.queryByText(OUTAGE)).not.toBeInTheDocument();
  });

  it("says a detail view needs a configured cluster on a 501", () => {
    render(
      <ErrorView
        error={
          new ApiRequestError(
            NODE_PATH,
            501,
            "detail views are not available without a cluster connection",
          )
        }
      />,
    );

    expect(
      screen.getByText("Indisponible sans connexion au cluster"),
    ).toBeInTheDocument();
    expect(screen.queryByText(OUTAGE)).not.toBeInTheDocument();
  });

  it("calls a rejected parameter what it is", () => {
    render(
      <ErrorView
        error={new ApiRequestError(NODE_PATH, 400, "vmid must be a positive integer")}
      />,
    );

    expect(screen.getByText("Requête invalide")).toBeInTheDocument();
    expect(screen.queryByText(OUTAGE)).not.toBeInTheDocument();
  });

  it("puts an unclassified 5xx on moxy itself", () => {
    render(<ErrorView error={new ApiRequestError(NODE_PATH, 500, null)} />);

    expect(screen.getByText("Erreur interne de moxy")).toBeInTheDocument();
    expect(screen.queryByText(OUTAGE)).not.toBeInTheDocument();
  });

  it("suspects a proxy when the body is not the JSON we asked for", () => {
    render(
      <ErrorView error={new ApiParseError(`GET ${NODE_PATH} returned a body that is not valid JSON`)} />,
    );

    expect(screen.getByText("Réponse inattendue")).toBeInTheDocument();
    expect(screen.getByText(/page HTML/)).toBeInTheDocument();
    expect(screen.queryByText(OUTAGE)).not.toBeInTheDocument();
  });

  it("keeps the generic wording for a plain error", () => {
    render(<ErrorView error={new Error("boom")} />);

    expect(screen.getByText("Impossible de charger les données")).toBeInTheDocument();
    expect(screen.queryByText(OUTAGE)).not.toBeInTheDocument();
  });
});

describe("ErrorView way out", () => {
  it("offers the overview only for a vanished object", () => {
    const onBack = vi.fn();
    const { rerender } = render(
      <ErrorView error={new ApiRequestError(NODE_PATH, 404, "not found")} onBack={onBack} />,
    );

    fireEvent.click(screen.getByRole("button", { name: "Retour à la vue d’ensemble" }));
    expect(onBack).toHaveBeenCalledTimes(1);

    // Retrying a cluster that is merely unreachable does work, so the way out
    // is not offered there: the screen fills itself when the cluster answers.
    rerender(
      <ErrorView
        error={new ApiRequestError(NODE_PATH, 502, "upstream unavailable")}
        onBack={onBack}
      />,
    );
    expect(
      screen.queryByRole("button", { name: "Retour à la vue d’ensemble" }),
    ).toBeNull();
  });

  it("offers nothing when no handler is given", () => {
    render(<ErrorView error={new ApiRequestError(NODE_PATH, 404, "not found")} />);

    expect(
      screen.queryByRole("button", { name: "Retour à la vue d’ensemble" }),
    ).toBeNull();
  });
});
