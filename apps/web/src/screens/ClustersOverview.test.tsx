import {
  fireEvent,
  getDefaultNormalizer,
  render,
  screen,
} from "@testing-library/react";

import type { ClusterOverview, Overview } from "@/api/types";
import { formatAlert, formatUsage } from "@/lib/format";

import { ClustersOverview } from "./ClustersOverview";

/**
 * `getByText` collapses every run of whitespace, and U+202F — the narrow
 * no-break space before a `%` — is whitespace to that normalizer. The alert
 * sentences carry one.
 */
const EXACT = { normalizer: getDefaultNormalizer({ collapseWhitespace: false }) };

const GIB = 1024 ** 3;
const TIB = 1024 ** 4;

function node(
  name: string,
  status: ClusterOverview["nodes"][number]["status"] = "online",
): ClusterOverview["nodes"][number] {
  return {
    name,
    status,
    uptime: 41 * 86400,
    cpu: { ratio: 0.04, cores: 32 },
    memory: { used: 20 * GIB, total: 128 * GIB, ratio: 20 / 128 },
    pendingUpdates: null,
    guests: [],
  };
}

const qualification: ClusterOverview = {
  id: "qual",
  name: "Qualification",
  color: null,
  status: "healthy",
  fetchedAt: "2026-09-12T10:00:00Z",
  error: null,
  quorum: { quorate: true, nodes: 3, online: 3 },
  cpu: { ratio: 0.04, cores: 96 },
  memory: { used: 61 * GIB, total: 384 * GIB, ratio: 61 / 384 },
  storage: { used: 1.2 * TIB, total: 5.4 * TIB, ratio: 1.2 / 5.4 },
  vms: { running: 12, stopped: 0, templates: 1, total: 12 },
  nodes: [
    node("prox-qual-2201-cit"),
    node("prox-qual-2202-cit"),
    node("prox-qual-2203-cit"),
  ],
  updates: null,
  alerts: [],
};

const preproduction: ClusterOverview = {
  id: "pprd",
  name: "Préproduction",
  color: null,
  status: "degraded",
  fetchedAt: "2026-09-12T10:00:00Z",
  error: null,
  quorum: { quorate: true, nodes: 3, online: 3 },
  cpu: { ratio: 0.31, cores: 72 },
  memory: { used: 212 * GIB, total: 256 * GIB, ratio: 0.828 },
  storage: { used: 3.9 * TIB, total: 8 * TIB, ratio: 3.9 / 8 },
  vms: { running: 44, stopped: 2, templates: 0, total: 46 },
  nodes: [
    node("prox-pprd-2301-cit"),
    node("prox-pprd-2302-cit", "maintenance"),
    node("prox-pprd-2303-cit"),
  ],
  updates: null,
  alerts: [{ kind: "memory_high", ratio: 0.828, nodes: ["prox-pprd-2301-cit", "prox-pprd-2303-cit"] }],
};

const production: ClusterOverview = {
  id: "prod",
  name: "Production",
  color: null,
  status: "healthy",
  fetchedAt: "2026-09-12T10:00:00Z",
  error: null,
  quorum: { quorate: true, nodes: 5, online: 5 },
  cpu: { ratio: 0.22, cores: 320 },
  memory: { used: 418 * GIB, total: 1024 * GIB, ratio: 418 / 1024 },
  storage: { used: 14 * TIB, total: 32 * TIB, ratio: 14 / 32 },
  vms: { running: 89, stopped: 0, templates: 3, total: 89 },
  nodes: [
    node("prox-prod-2401-cit"),
    node("prox-prod-2402-cit"),
    node("prox-prod-2403-cit"),
    node("prox-prod-2404-cit"),
    node("prox-prod-2405-cit"),
  ],
  updates: {
    nodes: [
      "prox-prod-2401-cit",
      "prox-prod-2402-cit",
      "prox-prod-2403-cit",
      "prox-prod-2404-cit",
      "prox-prod-2405-cit",
    ],
    pveManagerVersion: "9.2.12",
    checkedAt: "2026-09-12T10:00:00Z",
  },
  alerts: [
    {
      kind: "updates_available",
      version: "9.2.12",
      nodes: [
        "prox-prod-2401-cit",
        "prox-prod-2402-cit",
        "prox-prod-2403-cit",
        "prox-prod-2404-cit",
        "prox-prod-2405-cit",
      ],
    },
  ],
};

/** The three cards of appendix A.4, with the totals of its header. */
function overview(patch: Partial<Overview> = {}): Overview {
  return {
    generatedAt: "2026-09-12T10:00:00Z",
    thresholds: { memory: 0.8, cpu: 0.8, storage: 0.8 },
    totals: { clusters: 3, nodes: 11, nodesOnline: 11, vms: 148, alerts: 2 },
    clusters: [qualification, preproduction, production],
    ...patch,
  };
}

describe("ClustersOverview", () => {
  it("renders the title and the totals of the mock", () => {
    render(<ClustersOverview overview={overview()} />);

    expect(screen.getByRole("heading", { name: "Clusters" })).toBeInTheDocument();
    expect(screen.getByText("11 nœuds")).toBeInTheDocument();
    expect(screen.getByText("148 VM")).toBeInTheDocument();
    expect(screen.getByText("2 alertes")).toBeInTheDocument();
  });

  it("hides the alert tag when nothing is wrong", () => {
    const data = overview({
      totals: { clusters: 3, nodes: 11, nodesOnline: 11, vms: 148, alerts: 0 },
    });
    render(<ClustersOverview overview={data} />);

    // Narrow on purpose: the cards' own banners legitimately say "aucune
    // alerte"; what must be absent is the header counter.
    expect(screen.queryByText(/^\d+ alertes?$/)).toBeNull();
  });

  it("uses the singular for a single node, vm and alert", () => {
    const data = overview({
      totals: { clusters: 1, nodes: 1, nodesOnline: 1, vms: 1, alerts: 1 },
      clusters: [qualification],
    });
    render(<ClustersOverview overview={data} />);

    expect(screen.getByText("1 nœud")).toBeInTheDocument();
    expect(screen.getByText("1 VM")).toBeInTheDocument();
    expect(screen.getByText("1 alerte")).toBeInTheDocument();
  });

  // A cluster is declared server-side; the screen must not suggest otherwise.
  it("offers no way to add a cluster", () => {
    render(<ClustersOverview overview={overview()} />);

    expect(screen.queryByRole("button", { name: /Ajouter un cluster/ })).toBeNull();
  });

  it("renders one card per cluster", () => {
    render(<ClustersOverview overview={overview()} />);

    expect(screen.getByRole("heading", { name: "Qualification" })).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "Préproduction" })).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "Production" })).toBeInTheDocument();
    expect(screen.getByText("Dégradé")).toBeInTheDocument();
    expect(screen.getAllByText("Sain")).toHaveLength(2);
  });

  it("lists the five nodes of production, none of them summarised away", () => {
    const { container } = render(<ClustersOverview overview={overview()} />);

    expect(screen.getByText("prox-prod-2402-cit")).toBeInTheDocument();
    expect(screen.getByText("prox-prod-2404-cit")).toBeInTheDocument();
    expect(screen.getByText("prox-prod-2405-cit")).toBeInTheDocument();
    expect(container.textContent).not.toContain("autres nœuds");
  });

  it("lays the cards out on a grid that collapses on a narrow screen", () => {
    const { container } = render(<ClustersOverview overview={overview()} />);

    const grid = container.querySelector(".grid");
    expect(grid).toHaveClass("grid-cols-1");
    expect(grid).toHaveClass("md:grid-cols-2");
    expect(grid).toHaveClass("xl:grid-cols-3");
  });

  it("forwards the thresholds of the payload to the cards", () => {
    const data = overview({
      thresholds: { memory: 0.9, cpu: 0.9, storage: 0.9 },
    });
    render(<ClustersOverview overview={data} />);

    // 0.9 sits above every memory ratio of the sample, so no figure warns.
    for (const cluster of data.clusters) {
      expect(
        screen.getByText(formatUsage(cluster.memory), {
          // Byte counts hold narrow no-break spaces, which the default
          // normalizer would collapse on one side of the comparison only.
          normalizer: getDefaultNormalizer({ collapseWhitespace: false }),
        }),
      ).toHaveClass("text-text-primary");
    }
  });

  it("reports the id of the cluster that was activated", () => {
    const onSelectCluster = vi.fn();
    render(
      <ClustersOverview overview={overview()} onSelectCluster={onSelectCluster} />,
    );

    fireEvent.click(screen.getByRole("button", { name: "Ouvrir Préproduction" }));
    expect(onSelectCluster).toHaveBeenCalledWith("pprd");
  });

  it("leaves the cards inert when no handler is given", () => {
    render(<ClustersOverview overview={overview()} />);

    expect(screen.queryByRole("button", { name: /^Ouvrir / })).toBeNull();
  });

  // One tab stop per card, so a grid of clusters costs one tab each to cross
  // rather than one per figure it shows.
  it("gives each card exactly one control", () => {
    render(<ClustersOverview overview={overview()} onSelectCluster={vi.fn()} />);

    const cards = screen.getAllByRole("article");
    expect(cards.length).toBeGreaterThan(1);
    expect(screen.getAllByRole("button", { name: /^Ouvrir / })).toHaveLength(cards.length);
  });

  it("renders an empty state rather than an empty grid", () => {
    const data = overview({
      totals: { clusters: 0, nodes: 0, nodesOnline: 0, vms: 0, alerts: 0 },
      clusters: [],
    });
    const { container } = render(<ClustersOverview overview={data} />);

    expect(screen.getByText("Aucun cluster configuré")).toBeInTheDocument();
    expect(container.querySelector(".grid")).toBeNull();
    expect(screen.getByText("0 nœuds")).toBeInTheDocument();
  });

  it("merges the className it receives", () => {
    const { container } = render(
      <ClustersOverview overview={overview()} className="px-5" />,
    );

    expect(container.firstElementChild).toHaveClass("px-5");
  });

  // The header counts every alert of every cluster. If a card shows fewer than
  // it holds, the two contradict each other on the same screen — which is what
  // sent an operator looking for a second alert on another card.
  it("shows as many banners as the header counts", () => {
    const data = overview({
      totals: { clusters: 3, nodes: 11, nodesOnline: 11, vms: 148, alerts: 3 },
      clusters: [
        qualification,
        {
          ...preproduction,
          alerts: [
            { kind: "memory_high", ratio: 0.92, nodes: ["prox-pprd-2301-cit"] },
            { kind: "updates_available", version: "9.2.12", nodes: ["prox-pprd-2301-cit"] },
          ],
        },
        production,
      ],
    });
    render(<ClustersOverview overview={data} />);

    const banners = data.clusters.flatMap((cluster) =>
      cluster.alerts.map((alert) => formatAlert(alert)),
    );
    expect(banners).toHaveLength(data.totals.alerts);
    for (const sentence of banners) {
      expect(screen.getByText(sentence, EXACT)).toBeInTheDocument();
    }
  });
});
