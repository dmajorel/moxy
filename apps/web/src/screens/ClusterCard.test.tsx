import {
  fireEvent,
  getDefaultNormalizer,
  render,
  screen,
  within,
} from "@testing-library/react";

import type { ClusterOverview, Series } from "@/api/types";
import { NNBSP } from "@/lib/format";

import { ClusterCard } from "./ClusterCard";

/**
 * `getByText` collapses every run of whitespace into a plain space, and U+202F
 * — the narrow no-break space format.ts puts before a `%` — is whitespace to
 * that normalizer. These assertions are precisely about that character, so the
 * collapsing is turned off rather than the expectation loosened.
 */
const EXACT = { normalizer: getDefaultNormalizer({ collapseWhitespace: false }) };

const GIB = 1024 ** 3;
const TIB = 1024 ** 4;

/** Qualification, as shown in appendix A.4: healthy, three nodes, quorum 3/3. */
function healthyCluster(overrides: Partial<ClusterOverview> = {}): ClusterOverview {
  return {
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
    ...overrides,
  };
}

/** Préproduction: degraded, memory at 82,8 %, one node in maintenance. */
function degradedCluster(overrides: Partial<ClusterOverview> = {}): ClusterOverview {
  return healthyCluster({
    id: "pprd",
    name: "Préproduction",
    status: "degraded",
    cpu: { ratio: 0.31, cores: 72 },
    memory: { used: 212 * GIB, total: 256 * GIB, ratio: 0.828 },
    storage: { used: 3.9 * TIB, total: 8 * TIB, ratio: 3.9 / 8 },
    vms: { running: 44, stopped: 2, templates: 0, total: 46 },
    nodes: [
      node("prox-pprd-2301-cit"),
      node("prox-pprd-2302-cit", "maintenance"),
      node("prox-pprd-2303-cit"),
    ],
    // The ratio is that of the nodes named, not of the cluster: both sit at
    // 100 of 112 GiB while the cluster, drained node included, is at 82.8 %.
    alerts: [
      { kind: "memory_high", ratio: 100 / 112, nodes: ["a", "b"] },
    ],
    ...overrides,
  });
}

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

/** The label/value line of the CPU metric, label and suffix included. */
function cpuRow(): HTMLElement {
  const row = screen.getByText("CPU").closest("div");
  if (row === null) throw new Error("no cpu row");
  return row;
}

describe("ClusterCard", () => {
  it("renders the header with the status dot, the name and the status tag", () => {
    render(<ClusterCard cluster={healthyCluster()} threshold={0.8} />);

    expect(
      screen.getByRole("heading", { name: "Qualification" }),
    ).toBeInTheDocument();
    expect(screen.getByText("Sain")).toBeInTheDocument();
    expect(screen.getByRole("img", { name: "Sain" })).toBeInTheDocument();
  });

  it("frames a degraded cluster in amber and a healthy one with a hairline", () => {
    const degraded = render(<ClusterCard cluster={degradedCluster()} threshold={0.8} />);
    const degradedCard = degraded.container.firstElementChild;
    expect(degradedCard).toHaveClass("border-2");
    expect(degradedCard).toHaveClass("border-warning");

    const healthy = render(<ClusterCard cluster={healthyCluster()} threshold={0.8} />);
    const healthyCard = healthy.container.firstElementChild;
    expect(healthyCard).toHaveClass("border-border");
    expect(healthyCard).not.toHaveClass("border-warning");
  });

  it("marks an unreachable cluster with a muted dashed border, not with amber", () => {
    const { container } = render(
      <ClusterCard
        cluster={healthyCluster({ status: "unreachable" })}
        threshold={0.8}
      />,
    );

    const card = container.firstElementChild;
    expect(card).toHaveClass("border-dashed");
    expect(card).toHaveClass("border-text-muted");
    expect(card).not.toHaveClass("border-warning");
    expect(screen.getByText("Injoignable")).toBeInTheDocument();
  });

  it("renders the three metrics through the formatting layer", () => {
    render(<ClusterCard cluster={degradedCluster()} threshold={0.8} />);

    expect(screen.getByText(`31${NNBSP}%`, EXACT)).toBeInTheDocument();
    expect(screen.getByText("212 / 256 GiB")).toBeInTheDocument();
    expect(screen.getByText("3,9 / 8 TiB")).toBeInTheDocument();
  });

  it("renders unknown cpu and memory as a dash, never as 0 %", () => {
    // What a token without Sys.Audit on /nodes gets: nodes listed, no figures.
    render(
      <ClusterCard
        cluster={healthyCluster({
          cpu: null,
          memory: null,
          alerts: [{ kind: "node_stats_unavailable", nodes: ["a", "b", "c"] }],
        })}
        threshold={0.8}
      />,
    );

    expect(screen.getAllByText("—")).toHaveLength(2);
    expect(screen.queryByText(`0${NNBSP}%`, EXACT)).not.toBeInTheDocument();
    // Nothing to draw either: an empty chart says so rather than flat-lining
    // at zero, which would claim the cluster was idle.
    expect(
      screen.getByRole("img", { name: /utilisation.*aucune donnée/i }),
    ).toBeInTheDocument();
    expect(
      screen.getByText("Mesures CPU et mémoire indisponibles sur 3 nœuds"),
    ).toBeInTheDocument();
    // The cluster itself is fine: the banner is about moxy's token.
    expect(screen.getByText("Sain")).toBeInTheDocument();
  });

  it("passes the threshold down, so memory above it turns amber and cpu does not", () => {
    render(<ClusterCard cluster={degradedCluster()} threshold={0.8} />);

    // The bars are gone, the cue is not: the figure carries it now.
    expect(screen.getByText("212 / 256 GiB")).toHaveClass("text-text-warning-strong");
    expect(screen.getByText(`31${NNBSP}%`, EXACT)).toHaveClass("text-text-primary");
  });

  it("honours a threshold raised above the current memory ratio", () => {
    render(<ClusterCard cluster={degradedCluster()} threshold={0.9} />);

    expect(screen.getByText("212 / 256 GiB")).toHaveClass("text-text-primary");
  });

  it("builds the vm counter from the non-zero terms only", () => {
    render(<ClusterCard cluster={healthyCluster()} threshold={0.8} />);
    expect(screen.getByText("12 en cours · 1 template")).toBeInTheDocument();
  });

  it("pluralises the vm terms and drops the templates when there are none", () => {
    render(<ClusterCard cluster={degradedCluster()} threshold={0.8} />);
    expect(screen.getByText("44 en cours · 2 arrêtées")).toBeInTheDocument();
  });

  it("says so rather than showing a blank counter when the cluster has no vm", () => {
    render(
      <ClusterCard
        cluster={healthyCluster({
          vms: { running: 0, stopped: 0, templates: 0, total: 0 },
        })}
        threshold={0.8}
      />,
    );

    expect(screen.getByText("Aucune VM")).toBeInTheDocument();
  });

  it("heads the node list, so it does not read as the detail of the vm line", () => {
    render(<ClusterCard cluster={healthyCluster()} threshold={0.8} />);

    const heading = screen.getByRole("heading", { name: "Nœuds" });
    expect(heading).toBeInTheDocument();
    expect(screen.getByRole("list", { name: "Nœuds" })).toBeInTheDocument();
  });

  it("puts the node heading between the vm line and the first node", () => {
    const { container } = render(
      <ClusterCard cluster={healthyCluster()} threshold={0.8} />,
    );

    const text = container.textContent ?? "";
    expect(text.indexOf("12 en cours · 1 template")).toBeLessThan(
      text.indexOf("Nœuds"),
    );
    expect(text.indexOf("Nœuds")).toBeLessThan(text.indexOf("prox-qual-2201-cit"));
  });

  it("tags the node that is in maintenance", () => {
    render(<ClusterCard cluster={degradedCluster()} threshold={0.8} />);

    expect(screen.getByText("prox-pprd-2302-cit")).toBeInTheDocument();
    expect(screen.getByText("Maintenance")).toBeInTheDocument();
  });

  it("lists every node, however many the cluster has", () => {
    const names = [
      "prox-prod-2401-cit",
      "prox-prod-2402-cit",
      "prox-prod-2403-cit",
      "prox-prod-2404-cit",
      "prox-prod-2405-cit",
      "prox-prod-2406-cit",
    ];
    const cluster = healthyCluster({
      id: "prod",
      name: "Production",
      nodes: names.map((name) => node(name)),
    });
    const { container } = render(<ClusterCard cluster={cluster} threshold={0.8} />);

    for (const name of names) {
      expect(screen.getByText(name)).toBeInTheDocument();
    }
    expect(container.querySelectorAll("li")).toHaveLength(names.length);
  });

  it("no longer summarises the tail of the node list", () => {
    const cluster = healthyCluster({
      nodes: [
        node("prox-qual-2201-cit"),
        node("prox-qual-2202-cit"),
        node("prox-qual-2203-cit"),
        node("prox-qual-2204-cit"),
      ],
    });
    const { container } = render(<ClusterCard cluster={cluster} threshold={0.8} />);

    expect(screen.getByText("prox-qual-2204-cit")).toBeInTheDocument();
    expect(container.textContent).not.toContain("autre nœud");
    expect(container.textContent).not.toContain("autres nœuds");
  });

  it("keeps a node in maintenance visible past the old three-node cut", () => {
    const cluster = healthyCluster({
      nodes: [
        node("prox-qual-2201-cit"),
        node("prox-qual-2202-cit"),
        node("prox-qual-2203-cit"),
        node("prox-qual-2204-cit", "maintenance"),
      ],
    });
    render(<ClusterCard cluster={cluster} threshold={0.8} />);

    expect(screen.getByText("prox-qual-2204-cit")).toBeInTheDocument();
    expect(screen.getByText("Maintenance")).toBeInTheDocument();
  });

  it("shows the first alert, formatted, in the footer banner", () => {
    render(<ClusterCard cluster={degradedCluster()} threshold={0.8} />);

    expect(
      screen.getByText(`Mémoire à 89${NNBSP}% sur 2 nœuds (max.)`, EXACT),
    ).toBeInTheDocument();
  });

  // The banner used to quote the cluster average next to a list of nodes, so a
  // cluster at 55 % holding one node at 92 % announced "Mémoire à 55 % sur
  // 1 nœud" -- a figure that is true of nothing the sentence names.
  it("quotes the memory of the node it names, not the cluster average", () => {
    render(
      <ClusterCard
        cluster={healthyCluster({
          memory: { used: 55 * GIB, total: 100 * GIB, ratio: 0.55 },
          alerts: [{ kind: "memory_high", ratio: 0.92, nodes: ["prox-qual-2201-cit"] }],
        })}
        threshold={0.8}
      />,
    );

    expect(
      screen.getByText(`Mémoire à 92${NNBSP}% sur 1 nœud (max.)`, EXACT),
    ).toBeInTheDocument();
    expect(screen.queryByText(/Mémoire à 55/)).toBeNull();
  });

  // "hors ligne" is something the cluster said; a node known from
  // /cluster/resources alone has simply not been mentioned by anything
  // authoritative, which is a few seconds of any node that just joined.
  it("does not announce an unknown node as offline", () => {
    render(
      <ClusterCard
        cluster={healthyCluster({
          alerts: [{ kind: "node_unknown", nodes: ["prox-qual-2204-cit"] }],
        })}
        threshold={0.8}
      />,
    );

    expect(screen.getByText("1 nœud dans un état inconnu")).toBeInTheDocument();
    expect(screen.queryByText(/hors ligne/)).toBeNull();
  });

  it("uses the refresh glyph for a pending update alert", () => {
    const { container } = render(
      <ClusterCard
        cluster={healthyCluster({
          alerts: [
            {
              kind: "updates_available",
              version: "9.2.12",
              nodes: ["1", "2", "3", "4", "5"],
            },
          ],
        })}
        threshold={0.8}
      />,
    );

    expect(
      screen.getByText("Mise à jour 9.2.12 disponible sur 5 nœuds"),
    ).toBeInTheDocument();
    expect(container.querySelector(".tabler-icon-refresh")).not.toBeNull();
  });

  it("keeps the warning glyph for an uneven-updates alert", () => {
    const { container } = render(
      <ClusterCard
        cluster={healthyCluster({
          alerts: [{ kind: "updates_uneven", pendingMin: 8, pendingMax: 14 }],
        })}
        threshold={0.8}
      />,
    );

    expect(
      screen.getByText(
        "Mises à jour inégales : de 8 à 14 paquets en attente selon les nœuds",
      ),
    ).toBeInTheDocument();
    // The refresh glyph is reserved for the pending-update news.
    expect(container.querySelector(".tabler-icon-refresh")).toBeNull();
  });

  // A card shows alerts[0] only, and the backend puts the fault first.
  it("shows the uneven-updates banner ahead of the pending-update one", () => {
    render(
      <ClusterCard
        cluster={healthyCluster({
          alerts: [
            { kind: "updates_uneven", pendingMin: 8, pendingMax: 14 },
            { kind: "updates_available", version: "9.2.12", nodes: ["1", "2"] },
          ],
        })}
        threshold={0.8}
      />,
    );

    expect(
      screen.getByText(
        "Mises à jour inégales : de 8 à 14 paquets en attente selon les nœuds",
      ),
    ).toBeInTheDocument();
    expect(screen.queryByText(/Mise à jour 9.2.12 disponible/)).toBeNull();
  });

  it("falls back to the quorum when there is no alert", () => {
    render(<ClusterCard cluster={healthyCluster()} threshold={0.8} />);

    expect(screen.getByText("Quorum 3/3 · aucune alerte")).toBeInTheDocument();
  });

  it("invents no quorum for a standalone cluster", () => {
    const { container } = render(
      <ClusterCard cluster={healthyCluster({ quorum: null })} threshold={0.8} />,
    );

    expect(screen.getByText("Aucune alerte")).toBeInTheDocument();
    expect(container.textContent).not.toContain("Quorum");
  });

  it("shows how old the reading is", () => {
    const cluster = healthyCluster({
      fetchedAt: new Date(Date.now() - 3 * 60 * 1000).toISOString(),
    });
    render(<ClusterCard cluster={cluster} threshold={0.8} />);

    expect(screen.getByText("il y a 3 min")).toBeInTheDocument();
  });

  it("says the reading is old on error, without leaking the english message", () => {
    const cluster = healthyCluster({
      status: "unreachable",
      fetchedAt: new Date(Date.now() - 3 * 60 * 1000).toISOString(),
      error: {
        kind: "network",
        status: null,
        message: "dial tcp 10.0.0.1:8006: connect: connection refused",
      },
    });
    const { container } = render(<ClusterCard cluster={cluster} threshold={0.8} />);

    expect(
      screen.getByText("Lecture ancienne · il y a 3 min · réseau injoignable"),
    ).toBeInTheDocument();
    expect(container.textContent).not.toContain("connection refused");
    expect(container.textContent).not.toContain("dial tcp");
  });

  it("reports that nothing was ever read when there is no reading at all", () => {
    const cluster = healthyCluster({
      status: "unreachable",
      fetchedAt: null,
      error: { kind: "timeout", status: null, message: "context deadline exceeded" },
    });
    const { container } = render(<ClusterCard cluster={cluster} threshold={0.8} />);

    expect(screen.getByText("Aucune lecture disponible · délai dépassé")).toBeInTheDocument();
    expect(container.textContent).not.toContain("context deadline exceeded");
  });

  // "auth" alone covers two opposite errands: a token that is no longer valid
  // and a token that never had the privilege. The status tells them apart, and
  // the card is where an operator reads it.
  it.each([
    [401, "jeton refusé"],
    [403, "droits insuffisants"],
    [null, "authentification refusée"],
  ])("names what went wrong for an auth failure with status %s", (status, expected) => {
    const cluster = healthyCluster({
      status: "unreachable",
      fetchedAt: new Date(Date.now() - 60 * 1000).toISOString(),
      error: { kind: "auth", status, message: "http 403 Forbidden" },
    });
    render(<ClusterCard cluster={cluster} threshold={0.8} />);

    expect(screen.getByText(new RegExp(expected))).toBeInTheDocument();
  });

  it("stays inert when no onSelect is given", () => {
    render(<ClusterCard cluster={healthyCluster()} threshold={0.8} />);

    expect(screen.queryByRole("button")).toBeNull();
  });

  it("is activated by a click and by the Enter and Space keys", () => {
    const onSelect = vi.fn();
    render(
      <ClusterCard cluster={healthyCluster()} threshold={0.8} onSelect={onSelect} />,
    );

    const card = screen.getByRole("button", { name: "Cluster Qualification" });
    fireEvent.click(card);
    expect(onSelect).toHaveBeenCalledTimes(1);

    fireEvent.keyDown(card, { key: "Enter" });
    expect(onSelect).toHaveBeenCalledTimes(2);

    fireEvent.keyDown(card, { key: " " });
    expect(onSelect).toHaveBeenCalledTimes(3);

    fireEvent.keyDown(card, { key: "a" });
    expect(onSelect).toHaveBeenCalledTimes(3);
  });

  it("is reachable with the keyboard when it is activable", () => {
    render(
      <ClusterCard cluster={healthyCluster()} threshold={0.8} onSelect={vi.fn()} />,
    );

    expect(screen.getByRole("button")).toHaveAttribute("tabindex", "0");
  });

  it("merges the className it receives", () => {
    const { container } = render(
      <ClusterCard cluster={healthyCluster()} threshold={0.8} className="h-full" />,
    );

    expect(container.firstElementChild).toHaveClass("h-full");
    expect(container.firstElementChild).toHaveClass("rounded-panel");
  });
});

describe("cluster cpu total", () => {
  it("writes the processor count the load is a fraction of, next to it", () => {
    render(<ClusterCard cluster={degradedCluster()} threshold={0.8} />);

    expect(screen.getByText(`31${NNBSP}%`, EXACT)).toBeInTheDocument();
    expect(screen.getByText("· 72 c")).toBeInTheDocument();
  });

  it("writes the count quieter than the value it qualifies", () => {
    render(<ClusterCard cluster={degradedCluster()} threshold={0.8} />);

    expect(screen.getByText("· 72 c")).toHaveClass("text-[11px]", "text-text-muted");
  });

  it("formats the count through the formatting layer", () => {
    render(
      <ClusterCard
        cluster={healthyCluster({ cpu: { ratio: 0.04, cores: 1024 } })}
        threshold={0.8}
      />,
    );

    expect(screen.getByText(`· 1${NNBSP}024 c`, EXACT)).toBeInTheDocument();
  });

  it("adds no suffix when no node reported, rather than a second em dash", () => {
    render(
      <ClusterCard
        cluster={healthyCluster({ cpu: null, memory: null })}
        threshold={0.8}
      />,
    );

    expect(cpuRow().textContent).toBe("CPU—");
  });

  it("reads a zero count as unknown, not as a cluster without a processor", () => {
    // PVE lists a node without maxcpu when the token may not audit it.
    render(
      <ClusterCard
        cluster={healthyCluster({ cpu: { ratio: 0, cores: 0 } })}
        threshold={0.8}
      />,
    );

    expect(cpuRow().textContent).toBe(`CPU0${NNBSP}%`);
  });

  it("qualifies the cpu line only, not the memory and storage ones", () => {
    render(<ClusterCard cluster={degradedCluster()} threshold={0.8} />);

    expect(screen.getByText("212 / 256 GiB").parentElement?.textContent).toBe(
      "Mémoire212 / 256 GiB",
    );
    expect(screen.getByText("3,9 / 8 TiB").parentElement?.textContent).toBe(
      "Stockage3,9 / 8 TiB",
    );
  });
});

describe("node uptime in the list", () => {
  it("shows each node's uptime beside its name", () => {
    render(
      <ClusterCard
        cluster={healthyCluster({ nodes: [node("prox-qual-2201-cit")] })}
        threshold={0.8}
      />,
    );

    const row = screen.getByText("prox-qual-2201-cit").closest("li");
    expect(row).not.toBeNull();
    expect(within(row as HTMLElement).getByText("41 j")).toBeInTheDocument();
  });

  it("shows an em dash for an offline node rather than claiming it just booted", () => {
    // PVE reports uptime 0 for a node it cannot reach.
    const offline = { ...node("prox-qual-2202-cit", "offline"), uptime: 0 };
    render(<ClusterCard cluster={healthyCluster({ nodes: [offline] })} threshold={0.8} />);

    const row = screen.getByText("prox-qual-2202-cit").closest("li");
    expect(within(row as HTMLElement).getByText("—")).toBeInTheDocument();
    expect(within(row as HTMLElement).queryByText(/0\s*s/)).not.toBeInTheDocument();
  });

  it("keeps the uptime of a node in maintenance, alongside its tag", () => {
    // A drained node is still up: it refuses new guests, it did not restart.
    const drained = node("prox-pprd-2302-cit", "maintenance");
    render(<ClusterCard cluster={degradedCluster({ nodes: [drained] })} threshold={0.8} />);

    const row = screen.getByText("prox-pprd-2302-cit").closest("li");
    expect(within(row as HTMLElement).getByText("41 j")).toBeInTheDocument();
    expect(within(row as HTMLElement).getByText("Maintenance")).toBeInTheDocument();
  });

  it("shows an em dash for an unknown node", () => {
    const ghost = node("prox-qual-2203-cit", "unknown");
    render(<ClusterCard cluster={healthyCluster({ nodes: [ghost] })} threshold={0.8} />);

    const row = screen.getByText("prox-qual-2203-cit").closest("li");
    expect(within(row as HTMLElement).getByText("—")).toBeInTheDocument();
  });
});

describe("ClusterCard usage chart", () => {
  function usage(...cpu: (number | null)[]): Series {
    return {
      cluster: "pprd",
      timeframe: "hour",
      fetchedAt: "2026-09-12T10:00:00Z",
      cpuAverage: 0.3,
      points: cpu.map((value, index) => ({
        time: `2026-09-12T09:${String(index).padStart(2, "0")}:00Z`,
        cpu: value,
        memUsed: value === null ? null : 64 * 1024 ** 3,
        memTotal: 128 * 1024 ** 3,
        netIn: null,
        netOut: null,
      })),
    };
  }

  it("draws the hour in place of the cpu and memory gauges", () => {
    const { container } = render(
      <ClusterCard
        cluster={degradedCluster()}
        usage={usage(0.2, 0.4, 0.3)}
        threshold={0.8}
      />,
    );

    // Two curves, one per metric, on one chart.
    expect(container.querySelectorAll("polyline")).toHaveLength(2);
    expect(screen.getByText("Dernière heure")).toBeInTheDocument();
    // The gauges are gone; storage keeps the bar it has no history for.
    expect(screen.queryByRole("progressbar", { name: "CPU" })).not.toBeInTheDocument();
    expect(screen.queryByRole("progressbar", { name: "Mémoire" })).not.toBeInTheDocument();
    expect(screen.getByRole("progressbar", { name: "Stockage" })).toBeInTheDocument();
  });

  it("keeps the instantaneous figures beside the curves", () => {
    // An hour says where the cluster is heading, not where it is.
    render(
      <ClusterCard cluster={degradedCluster()} usage={usage(0.2, 0.4)} threshold={0.8} />,
    );

    expect(screen.getByText(`31${NNBSP}%`, EXACT)).toBeInTheDocument();
    expect(screen.getByText("212 / 256 GiB")).toBeInTheDocument();
  });

  it("names both metrics in the chart's accessible label", () => {
    // Nothing is carried by colour alone: the curves are named in words, and
    // so is what they currently read.
    render(
      <ClusterCard cluster={degradedCluster()} usage={usage(0.2, 0.4)} threshold={0.8} />,
    );

    expect(
      screen.getByRole("img", { name: /Utilisation de Préproduction sur la dernière heure/ }),
    ).toBeInTheDocument();
  });

  it("breaks the curve on a gap rather than drawing it at zero", () => {
    const { container } = render(
      <ClusterCard
        cluster={degradedCluster()}
        usage={usage(0.2, null, 0.4)}
        threshold={0.8}
      />,
    );

    // Two runs for the cpu curve, one for the memory curve, which has the same
    // hole: four polylines would mean the gap was drawn through.
    expect(container.querySelectorAll("polyline")).toHaveLength(4);
  });

  it("says it has nothing to draw while the hour has not arrived", () => {
    // The card is served either way: a chart that could not be fetched costs
    // the curve, never the figures or the node list.
    render(<ClusterCard cluster={degradedCluster()} threshold={0.8} />);

    expect(
      screen.getByRole("img", { name: /utilisation.*aucune donnée/i }),
    ).toBeInTheDocument();
    expect(screen.getByText(`31${NNBSP}%`, EXACT)).toBeInTheDocument();
  });
});
