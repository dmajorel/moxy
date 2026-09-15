import {
  fireEvent,
  getDefaultNormalizer,
  render,
  screen,
  within,
} from "@testing-library/react";

import type { ClusterOverview, Series, Thresholds } from "@/api/types";
import { NNBSP } from "@/lib/format";

import { ClusterCard } from "./ClusterCard";

/**
 * One figure for the three resources, which is what the defaults are: a test
 * that needs them apart says so on the spot.
 */
function evenly(ratio: number): Thresholds {
  return { memory: ratio, cpu: ratio, storage: ratio };
}

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
    // The backend sends null for a node with no uptime to report, which is
    // what an offline or unknown node is: the card no longer decides that for
    // itself, it renders what the payload says.
    uptime: status === "offline" || status === "unknown" ? null : 41 * 86400,
    cpu: { ratio: 0.04, cores: 32 },
    memory: { used: 20 * GIB, total: 128 * GIB, ratio: 20 / 128 },
    pendingUpdates: null,
    guests: [],
  };
}

/** A node carrying the load, in a cluster whose average says otherwise. */
function hotNode(name: string): ClusterOverview["nodes"][number] {
  return {
    ...node(name),
    cpu: { ratio: 0.95, cores: 32 },
    memory: { used: 117 * GIB, total: 128 * GIB, ratio: 117 / 128 },
  };
}

/**
 * The accent of the header, which is the only element there carrying a style
 * attribute — the chart draws its own further down the card.
 */
function headerAccent(): HTMLElement | null {
  const heading = screen.getByRole("heading", { level: 3 });
  return heading.parentElement?.querySelector("[style]") ?? null;
}

/**
 * The label/value line of the CPU metric, label and suffix included.
 *
 * "CPU" also heads a column of the node table below, so the legend is the one
 * outside it.
 */
function cpuRow(): HTMLElement {
  const label = screen
    .getAllByText("CPU")
    .find((element) => element.closest("table") === null);
  const row = label?.closest("div") ?? null;
  if (row === null) throw new Error("no cpu row");
  return row;
}

/** The <tr> of one node, so its own figures can be told from its neighbours'. */
function nodeRow(name: string): HTMLElement {
  const row = screen.getByText(name).closest("tr");
  if (row === null) throw new Error(`no row for node ${name}`);
  return row;
}

describe("ClusterCard", () => {
  it("renders the header with the status dot, the name and the status tag", () => {
    render(<ClusterCard cluster={healthyCluster()} thresholds={evenly(0.8)} />);

    expect(
      screen.getByRole("heading", { name: "Qualification" }),
    ).toBeInTheDocument();
    expect(screen.getByText("Sain")).toBeInTheDocument();
    expect(screen.getByRole("img", { name: "Sain" })).toBeInTheDocument();
  });

  it("marks the card with the accent the cluster was configured with", () => {
    render(
      <ClusterCard cluster={healthyCluster({ color: "#7C5CD6" })} thresholds={evenly(0.8)} />,
    );

    const mark = headerAccent();
    expect(mark).not.toBeNull();
    expect(mark?.style.getPropertyValue("--cluster-accent")).toBe("#7C5CD6");
    // Decoration only: the heading right beside it already names the cluster.
    expect(mark).toHaveAttribute("aria-hidden", "true");
  });

  it("marks nothing when no accent was configured", () => {
    render(<ClusterCard cluster={healthyCluster()} thresholds={evenly(0.8)} />);

    expect(headerAccent()).toBeNull();
  });

  it("frames a degraded cluster in amber and a healthy one with a hairline", () => {
    const degraded = render(<ClusterCard cluster={degradedCluster()} thresholds={evenly(0.8)} />);
    const degradedCard = degraded.container.firstElementChild;
    expect(degradedCard).toHaveClass("border-2");
    expect(degradedCard).toHaveClass("border-warning");

    const healthy = render(<ClusterCard cluster={healthyCluster()} thresholds={evenly(0.8)} />);
    const healthyCard = healthy.container.firstElementChild;
    expect(healthyCard).toHaveClass("border-border");
    expect(healthyCard).not.toHaveClass("border-warning");
  });

  it("marks an unreachable cluster with a muted dashed border, not with amber", () => {
    const { container } = render(
      <ClusterCard
        cluster={healthyCluster({ status: "unreachable" })}
        thresholds={evenly(0.8)}
      />,
    );

    const card = container.firstElementChild;
    expect(card).toHaveClass("border-dashed");
    expect(card).toHaveClass("border-text-muted");
    expect(card).not.toHaveClass("border-warning");
    expect(screen.getByText("Injoignable")).toBeInTheDocument();
  });

  it("renders the three metrics through the formatting layer", () => {
    render(<ClusterCard cluster={degradedCluster()} thresholds={evenly(0.8)} />);

    expect(screen.getByText(`31${NNBSP}%`, EXACT)).toBeInTheDocument();
    // Memory and storage lead with how full they are and keep the volume
    // behind it: the division is no longer left to the reader.
    expect(screen.getByText(`83${NNBSP}%`, EXACT)).toBeInTheDocument();
    expect(screen.getByText("· 212 / 256 GiB")).toBeInTheDocument();
    expect(screen.getByText(`49${NNBSP}%`, EXACT)).toBeInTheDocument();
    expect(screen.getByText("· 3,9 / 8 TiB")).toBeInTheDocument();
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
        thresholds={evenly(0.8)}
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
    render(<ClusterCard cluster={degradedCluster()} thresholds={evenly(0.8)} />);

    // The bars are gone, the cue is not: the figure carries it now -- the
    // percentage, which is what a threshold is a threshold on, while the
    // volume beside it stays muted.
    expect(screen.getByText(`83${NNBSP}%`, EXACT)).toHaveClass("text-text-warning-strong");
    expect(screen.getByText(`31${NNBSP}%`, EXACT)).toHaveClass("text-text-primary");
  });

  // A single threshold meant an operator raising the memory limit to 0,9 --
  // because their nodes idle at 85 % of RAM -- silently raised the storage bar
  // with it, and a cluster whose storage must warn at 70 % had no way to say so.
  it("colours each reading by the threshold of its own resource", () => {
    const cluster = degradedCluster({
      memory: { used: 218 * GIB, total: 256 * GIB, ratio: 0.85 },
      storage: { used: 6 * TIB, total: 8 * TIB, ratio: 0.75 },
    });
    render(
      <ClusterCard
        cluster={cluster}
        thresholds={{ memory: 0.9, cpu: 0.9, storage: 0.7 }}
      />,
    );

    // 85 % of RAM under a 0,9 limit: nothing to see.
    expect(screen.getByText(`85${NNBSP}%`, EXACT)).toHaveClass("text-text-primary");
    // 75 % of storage over a 0,7 limit: amber, on its own account.
    expect(screen.getByRole("progressbar", { name: "Stockage" }).firstElementChild)
      .toHaveClass("bg-warning");
  });

  it("honours a threshold raised above the current memory ratio", () => {
    render(<ClusterCard cluster={degradedCluster()} thresholds={evenly(0.9)} />);

    expect(screen.getByText(`83${NNBSP}%`, EXACT)).toHaveClass("text-text-primary");
  });

  it("builds the vm counter from the non-zero terms only", () => {
    render(<ClusterCard cluster={healthyCluster()} thresholds={evenly(0.8)} />);
    expect(screen.getByText("12 en cours · 1 modèle")).toBeInTheDocument();
  });

  it("pluralises the vm terms and drops the templates when there are none", () => {
    render(<ClusterCard cluster={degradedCluster()} thresholds={evenly(0.8)} />);
    expect(screen.getByText("44 en cours · 2 arrêtées")).toBeInTheDocument();
  });

  it("says so rather than showing a blank counter when the cluster has no vm", () => {
    render(
      <ClusterCard
        cluster={healthyCluster({
          vms: { running: 0, stopped: 0, templates: 0, total: 0 },
        })}
        thresholds={evenly(0.8)}
      />,
    );

    expect(screen.getByText("Aucune VM")).toBeInTheDocument();
  });

  it("heads the node list, so it does not read as the detail of the vm line", () => {
    render(<ClusterCard cluster={healthyCluster()} thresholds={evenly(0.8)} />);

    const heading = screen.getByRole("heading", { name: "Nœuds" });
    expect(heading).toBeInTheDocument();
    // The heading is for the eye, the caption for a screen reader; the table
    // must carry both rather than lean on the heading.
    expect(
      screen.getByRole("table", { name: "Nœuds de Qualification" }),
    ).toBeInTheDocument();
  });

  it("puts the node heading between the vm line and the first node", () => {
    const { container } = render(
      <ClusterCard cluster={healthyCluster()} thresholds={evenly(0.8)} />,
    );

    const text = container.textContent ?? "";
    expect(text.indexOf("12 en cours · 1 template")).toBeLessThan(
      text.indexOf("Nœuds"),
    );
    expect(text.indexOf("Nœuds")).toBeLessThan(text.indexOf("prox-qual-2201-cit"));
  });

  it("marks the node that is in maintenance with a wrench, not a badge", () => {
    render(<ClusterCard cluster={degradedCluster()} thresholds={evenly(0.8)} />);

    const row = screen.getByText("prox-pprd-2302-cit").closest("tr");
    // The state is still named — the word moved from the badge onto the glyph,
    // it was not dropped — and the badge is gone from the end of the cell. The
    // word is looked for in a span rather than anywhere: the <title> of the
    // svg, which is what a pointer reads as a tooltip, carries it too.
    const mark = within(row as HTMLElement).getByRole("img", { name: "Maintenance" });
    expect(mark).toHaveClass("text-text-warning-strong");
    expect(
      within(row as HTMLElement).queryByText("Maintenance", { selector: "span" }),
    ).not.toBeInTheDocument();
  });

  it("gives the wrench the place of the status dot, not a place beside it", () => {
    render(<ClusterCard cluster={degradedCluster()} thresholds={evenly(0.8)} />);

    // One mark per row and no other: a drained node showing both would be
    // saying its state twice, which is what this replaced.
    const drained = screen.getByText("prox-pprd-2302-cit").closest("tr");
    expect(within(drained as HTMLElement).getAllByRole("img")).toHaveLength(1);

    const up = screen.getByText("prox-pprd-2301-cit").closest("tr");
    expect(
      within(up as HTMLElement).getByRole("img", { name: "En ligne" }),
    ).toBeInTheDocument();
  });

  it("shows the cpu and memory of each node, which the cluster average hides", () => {
    // The whole point: the cluster reads quiet while one node is not.
    const cluster = healthyCluster({
      cpu: { ratio: 0.28, cores: 96 },
      nodes: [
        node("prox-qual-2201-cit"),
        hotNode("prox-qual-2202-cit"),
        node("prox-qual-2203-cit"),
      ],
    });

    render(<ClusterCard cluster={cluster} thresholds={evenly(0.8)} />);

    expect(nodeRow("prox-qual-2202-cit").textContent).toContain(`95${NNBSP}%`);
    expect(nodeRow("prox-qual-2202-cit").textContent).toContain(`91${NNBSP}%`);
    // Its quiet neighbours carry their own figures, not the cluster's.
    expect(nodeRow("prox-qual-2201-cit").textContent).toContain(`4${NNBSP}%`);
    expect(nodeRow("prox-qual-2201-cit").textContent).not.toContain(`28${NNBSP}%`);
  });

  it("turns a node past the threshold amber, and leaves the quiet ones alone", () => {
    const cluster = healthyCluster({
      nodes: [node("prox-qual-2201-cit"), hotNode("prox-qual-2202-cit")],
    });

    render(<ClusterCard cluster={cluster} thresholds={evenly(0.8)} />);

    const hot = within(nodeRow("prox-qual-2202-cit"));
    expect(hot.getByText(`95${NNBSP}%`, EXACT)).toHaveClass("text-text-warning-strong");
    expect(hot.getByText(`91${NNBSP}%`, EXACT)).toHaveClass("text-text-warning-strong");

    const quiet = within(nodeRow("prox-qual-2201-cit"));
    expect(quiet.getByText(`4${NNBSP}%`, EXACT)).toHaveClass("text-text-secondary");
  });

  it("colours a node by the threshold of its own resource", () => {
    // Memory at 0,9 keeps the 91 % node quiet; cpu at 0,8 does not hide the
    // 95 %. One reading must never be coloured by another's limit.
    const cluster = healthyCluster({ nodes: [hotNode("prox-qual-2202-cit")] });

    render(
      <ClusterCard
        cluster={cluster}
        thresholds={{ cpu: 0.8, memory: 0.95, storage: 0.8 }}
      />,
    );

    const row = within(nodeRow("prox-qual-2202-cit"));
    expect(row.getByText(`95${NNBSP}%`, EXACT)).toHaveClass("text-text-warning-strong");
    expect(row.getByText(`91${NNBSP}%`, EXACT)).toHaveClass("text-text-secondary");
  });

  it("renders a node without figures as a dash, never as 0 %", () => {
    // A node PVE lists without cpu/maxmem: offline, or beyond what the token
    // may audit. Unknown is not idle, and "0 %" would say it was.
    const cluster = healthyCluster({
      nodes: [{ ...node("prox-qual-2201-cit", "offline"), cpu: null, memory: null }],
    });

    render(<ClusterCard cluster={cluster} thresholds={evenly(0.8)} />);

    const cells = within(nodeRow("prox-qual-2201-cit")).getAllByRole("cell");
    expect(cells[1]).toHaveTextContent("—");
    expect(cells[2]).toHaveTextContent("—");
    expect(
      within(nodeRow("prox-qual-2201-cit")).queryByText(`0${NNBSP}%`, EXACT),
    ).not.toBeInTheDocument();
  });

  it("names every column of the node table, and scopes each header to it", () => {
    render(<ClusterCard cluster={healthyCluster()} thresholds={evenly(0.8)} />);

    // Without these the row is four values and no clue which is which: the
    // reader is left to guess that the first percentage is the processor.
    for (const name of ["Nœud", "CPU", "Mémoire", "En service"]) {
      const header = screen.getByRole("columnheader", { name });
      // `scope` is what ties a figure to its heading; a bare <th> does not.
      expect(header).toHaveAttribute("scope", "col");
    }
  });

  it("puts each figure of a row under its own column", () => {
    render(
      <ClusterCard
        cluster={healthyCluster({ nodes: [node("prox-qual-2201-cit")] })}
        thresholds={evenly(0.8)}
      />,
    );

    const cells = within(nodeRow("prox-qual-2201-cit")).getAllByRole("cell");
    // textContent, not toHaveTextContent: the latter collapses the narrow
    // no-break space these figures are precisely about.
    expect(cells[0]?.textContent).toContain("prox-qual-2201-cit");
    expect(cells[1]?.textContent).toBe(`4${NNBSP}%`);
    expect(cells[2]?.textContent).toBe(`16${NNBSP}%`);
    expect(cells[3]?.textContent).toBe("41 j");
  });

  // A node name is the identifier that gets retyped into an `ssh` or a
  // `ha-manager` command, and a cluster spells it with a shared prefix —
  // `prox-qual-2201-cit`, `prox-qual-2202-cit` — so a cut takes exactly the
  // characters that tell one node from the next.
  it("never cuts a node name, and needs no tooltip to give it back", () => {
    render(
      <ClusterCard
        cluster={healthyCluster({ nodes: [node("prox-qual-2201-cit")] })}
        thresholds={evenly(0.8)}
      />,
    );

    const name = screen.getByText("prox-qual-2201-cit");
    expect(name).not.toHaveClass("truncate");
    // Nothing is hidden, so nothing has to be revealed on hover — which the
    // title button's full-card overlay would swallow anyway.
    expect(name).not.toHaveAttribute("title");
    // The last resort for a hostname with nothing to break on: it wraps rather
    // than widening the table into a scroller no pointer can reach.
    expect(name).toHaveClass("break-words");
  });

  it("gives the spare width to the name, and rules the figures off from it", () => {
    render(
      <ClusterCard
        cluster={healthyCluster({ nodes: [node("prox-qual-2201-cit")] })}
        thresholds={evenly(0.8)}
      />,
    );

    const cells = within(nodeRow("prox-qual-2201-cit")).getAllByRole("cell");
    // The reverse of what it was: the name column is the one that stretches.
    expect(cells[0]).toHaveClass("w-full");
    expect(cells[0]).not.toHaveClass("max-w-0");
    for (const figure of cells.slice(1)) {
      expect(figure).toHaveClass("border-l-[0.5px]");
      expect(figure).toHaveClass("border-border");
      expect(figure).not.toHaveClass("w-full");
    }
    expect(cells[0]).not.toHaveClass("border-l-[0.5px]");
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
    const { container } = render(<ClusterCard cluster={cluster} thresholds={evenly(0.8)} />);

    for (const name of names) {
      expect(screen.getByText(name)).toBeInTheDocument();
    }
    // One row per node, the header row aside.
    expect(container.querySelectorAll("tbody tr")).toHaveLength(names.length);
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
    const { container } = render(<ClusterCard cluster={cluster} thresholds={evenly(0.8)} />);

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
    render(<ClusterCard cluster={cluster} thresholds={evenly(0.8)} />);

    expect(screen.getByText("prox-qual-2204-cit")).toBeInTheDocument();
    expect(screen.getByText("Maintenance")).toBeInTheDocument();
  });

  it("shows the first alert, formatted, in the banner under the name", () => {
    render(<ClusterCard cluster={degradedCluster()} thresholds={evenly(0.8)} />);

    expect(
      screen.getByText(`Mémoire à 89${NNBSP}% sur 2 nœuds (max.)`, EXACT),
    ).toBeInTheDocument();
  });

  // The assertions above find the sentence anywhere in the card, so they pass
  // just as well with the banner back at the foot. These ones are the only
  // thing standing between the card and that regression.
  it("puts the alert banner between the name and the usage chart", () => {
    const { container } = render(
      <ClusterCard
        cluster={degradedCluster({
          fetchedAt: new Date(Date.now() - 3 * 60 * 1000).toISOString(),
        })}
        thresholds={evenly(0.8)}
      />,
    );

    const text = container.textContent ?? "";
    const banner = text.indexOf("Mémoire à 89");
    expect(banner).toBeGreaterThan(text.indexOf("Préproduction"));
    expect(banner).toBeLessThan(text.indexOf("Dernière heure"));
    expect(banner).toBeLessThan(text.indexOf("Nœuds"));
    // The freshness line does not follow it up: it says how old the reading
    // is, not what to do about it.
    expect(text.indexOf("il y a 3 min")).toBeGreaterThan(text.indexOf("Nœuds"));
  });

  // Same place whether or not there is something to read, so the eye does not
  // have to hunt for the banner on a grid where one card in three is in alert.
  it("puts the quiet banner where the alert banner would be", () => {
    const { container } = render(
      <ClusterCard cluster={healthyCluster()} thresholds={evenly(0.8)} />,
    );

    const text = container.textContent ?? "";
    const banner = text.indexOf("Quorum 3/3");
    expect(banner).toBeGreaterThan(text.indexOf("Qualification"));
    expect(banner).toBeLessThan(text.indexOf("Dernière heure"));
    expect(banner).toBeLessThan(text.indexOf("Nœuds"));
  });

  // The header's bottom margin and the banner's top one used to be two rules
  // for one gap; adjacent, they doubled the card's spacing under the name.
  it("spaces the name and the banner with a single rule", () => {
    const { container } = render(
      <ClusterCard cluster={degradedCluster()} thresholds={evenly(0.8)} />,
    );

    const header = container.querySelector("h3")?.parentElement;
    expect(header?.className).not.toMatch(/\bmb-/);
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
        thresholds={evenly(0.8)}
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
        thresholds={evenly(0.8)}
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
        thresholds={evenly(0.8)}
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
        thresholds={evenly(0.8)}
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

  // The header counts every alert, and the card showed one: an operator went
  // looking for the second on another card and did not find it. Typically an
  // available update hidden behind a memory warning, which appendix A.4 draws
  // as a banner in its own right.
  it("shows every alert, not just the first", () => {
    render(
      <ClusterCard
        cluster={healthyCluster({
          alerts: [
            { kind: "memory_high", ratio: 0.92, nodes: ["prox-qual-2201-cit"] },
            { kind: "updates_uneven", pendingMin: 8, pendingMax: 14 },
            { kind: "updates_available", version: "9.2.12", nodes: ["1", "2"] },
          ],
        })}
        thresholds={evenly(0.8)}
      />,
    );

    expect(
      screen.getByText(`Mémoire à 92${NNBSP}% sur 1 nœud (max.)`, EXACT),
    ).toBeInTheDocument();
    expect(
      screen.getByText(
        "Mises à jour inégales : de 8 à 14 paquets en attente selon les nœuds",
      ),
    ).toBeInTheDocument();
    expect(screen.getByText("Mise à jour 9.2.12 disponible sur 2 nœuds")).toBeInTheDocument();
    // And the quiet line does not appear alongside them.
    expect(screen.queryByText(/aucune alerte/)).toBeNull();
  });

  // The backend orders them by severity and the card keeps that order: the
  // first banner is the one to read first.
  it("keeps the order the backend serves", () => {
    const { container } = render(
      <ClusterCard
        cluster={healthyCluster({
          alerts: [
            { kind: "updates_uneven", pendingMin: 8, pendingMax: 14 },
            { kind: "updates_available", version: "9.2.12", nodes: ["1", "2"] },
          ],
        })}
        thresholds={evenly(0.8)}
      />,
    );

    const text = container.textContent ?? "";
    expect(text.indexOf("Mises à jour inégales")).toBeLessThan(
      text.indexOf("Mise à jour 9.2.12 disponible"),
    );
  });

  // The refresh glyph belongs to the pending-update news alone, even when it
  // is stacked under a fault.
  it("gives each banner its own glyph", () => {
    const { container } = render(
      <ClusterCard
        cluster={healthyCluster({
          alerts: [
            { kind: "updates_uneven", pendingMin: 8, pendingMax: 14 },
            { kind: "updates_available", version: "9.2.12", nodes: ["1", "2"] },
          ],
        })}
        thresholds={evenly(0.8)}
      />,
    );

    expect(container.querySelectorAll(".tabler-icon-refresh")).toHaveLength(1);
  });

  it("falls back to the quorum when there is no alert", () => {
    render(<ClusterCard cluster={healthyCluster()} thresholds={evenly(0.8)} />);

    expect(screen.getByText("Quorum 3/3 · aucune alerte")).toBeInTheDocument();
  });

  it("invents no quorum for a standalone cluster", () => {
    const { container } = render(
      <ClusterCard cluster={healthyCluster({ quorum: null })} thresholds={evenly(0.8)} />,
    );

    expect(screen.getByText("Aucune alerte")).toBeInTheDocument();
    expect(container.textContent).not.toContain("Quorum");
  });

  it("shows how old the reading is", () => {
    const cluster = healthyCluster({
      fetchedAt: new Date(Date.now() - 3 * 60 * 1000).toISOString(),
    });
    render(<ClusterCard cluster={cluster} thresholds={evenly(0.8)} />);

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
    const { container } = render(<ClusterCard cluster={cluster} thresholds={evenly(0.8)} />);

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
    const { container } = render(<ClusterCard cluster={cluster} thresholds={evenly(0.8)} />);

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
    render(<ClusterCard cluster={cluster} thresholds={evenly(0.8)} />);

    expect(screen.getByText(new RegExp(expected))).toBeInTheDocument();
  });

  it("stays inert when no onSelect is given", () => {
    render(<ClusterCard cluster={healthyCluster()} thresholds={evenly(0.8)} />);

    expect(screen.queryByRole("button")).toBeNull();
  });

  // The title carries the activation, not the card.
  //
  // It is a REAL <button type="button">, which is the whole point: Enter and
  // Space are then the browser's business rather than a keydown handler
  // written by hand, and the previous card had to write one because an
  // <article> with role="button" gets none. jsdom does not synthesise the
  // browser's activation from a keydown either, so what is asserted here is
  // the element type -- which is what makes the keyboard work at all.
  it("opens the cluster from its title", () => {
    const onSelect = vi.fn();
    render(
      <ClusterCard cluster={healthyCluster()} thresholds={evenly(0.8)} onSelect={onSelect} />,
    );

    const open = screen.getByRole("button", { name: "Ouvrir Qualification" });
    expect(open.tagName).toBe("BUTTON");
    expect(open).toHaveAttribute("type", "button");
    // Focusable without a tabindex of its own: a native control already is.
    expect(open).not.toHaveAttribute("tabindex");
    expect(open).toHaveTextContent("Qualification");

    fireEvent.click(open);
    expect(onSelect).toHaveBeenCalledTimes(1);
  });

  // The mouse affordance the mockups draw is kept: the button's hit area is
  // stretched over the whole card by an ::after overlay, so a click anywhere
  // opens the cluster while there is still exactly one control.
  it("keeps the whole card clickable", () => {
    render(
      <ClusterCard cluster={healthyCluster()} thresholds={evenly(0.8)} onSelect={vi.fn()} />,
    );

    const open = screen.getByRole("button", { name: "Ouvrir Qualification" });
    expect(open.className).toContain("after:absolute");
    expect(open.className).toContain("after:inset-0");
    // The overlay is positioned against the card, which must say so.
    expect(screen.getByRole("article").className).toContain("relative");
  });

  // The card USED to be the button, and that is what made this screen
  // inaudible: role="button" has "Children Presentational: true", so assistive
  // technology announced "Cluster Qualification, bouton" and not one of the
  // figures the card exists to show.
  it("exposes its content instead of flattening it into one button", () => {
    render(
      <ClusterCard cluster={degradedCluster()} thresholds={evenly(0.8)} onSelect={vi.fn()} />,
    );

    const card = screen.getByRole("article");
    expect(card).toHaveAccessibleName("Préproduction");

    // Everything the card says is still in the accessibility tree.
    expect(within(card).getByRole("heading", { name: "Préproduction" })).toBeInTheDocument();
    expect(within(card).getByRole("heading", { name: "Nœuds" })).toBeInTheDocument();
    expect(within(card).getByRole("table")).toBeInTheDocument();
    expect(within(card).getByRole("progressbar", { name: /Stockage/ })).toBeInTheDocument();
    expect(within(card).getByText(/Mémoire à/)).toBeInTheDocument();
  });

  // Exactly one tab stop per card: a grid of eight clusters must not cost
  // eight tabs to cross, and a second control inside the card would be
  // unreachable under the title button's overlay anyway.
  it("has one focusable control", () => {
    render(
      <ClusterCard cluster={degradedCluster()} thresholds={evenly(0.8)} onSelect={vi.fn()} />,
    );

    expect(screen.getAllByRole("button")).toHaveLength(1);
  });

  it("is a plain region when it leads nowhere", () => {
    render(<ClusterCard cluster={healthyCluster()} thresholds={evenly(0.8)} />);

    expect(screen.queryByRole("button")).toBeNull();
    // Still named, still readable: only the way out of it is gone.
    expect(screen.getByRole("article")).toHaveAccessibleName("Qualification");
  });

  it("merges the className it receives", () => {
    const { container } = render(
      <ClusterCard cluster={healthyCluster()} thresholds={evenly(0.8)} className="h-full" />,
    );

    expect(container.firstElementChild).toHaveClass("h-full");
    expect(container.firstElementChild).toHaveClass("rounded-panel");
  });
});

describe("cluster cpu total", () => {
  it("writes the processor count the load is a fraction of, next to it", () => {
    render(<ClusterCard cluster={degradedCluster()} thresholds={evenly(0.8)} />);

    expect(screen.getByText(`31${NNBSP}%`, EXACT)).toBeInTheDocument();
    expect(screen.getByText("· 72 c")).toBeInTheDocument();
  });

  it("writes the count quieter than the value it qualifies", () => {
    render(<ClusterCard cluster={degradedCluster()} thresholds={evenly(0.8)} />);

    expect(screen.getByText("· 72 c")).toHaveClass("text-[11px]", "text-text-muted");
  });

  it("formats the count through the formatting layer", () => {
    render(
      <ClusterCard
        cluster={healthyCluster({ cpu: { ratio: 0.04, cores: 1024 } })}
        thresholds={evenly(0.8)}
      />,
    );

    expect(screen.getByText(`· 1${NNBSP}024 c`, EXACT)).toBeInTheDocument();
  });

  it("adds no suffix when no node reported, rather than a second em dash", () => {
    render(
      <ClusterCard
        cluster={healthyCluster({ cpu: null, memory: null })}
        thresholds={evenly(0.8)}
      />,
    );

    expect(cpuRow().textContent).toBe("CPU—");
  });

  it("reads a zero count as unknown, not as a cluster without a processor", () => {
    // PVE lists a node without maxcpu when the token may not audit it.
    render(
      <ClusterCard
        cluster={healthyCluster({ cpu: { ratio: 0, cores: 0 } })}
        thresholds={evenly(0.8)}
      />,
    );

    expect(cpuRow().textContent).toBe(`CPU0${NNBSP}%`);
  });

  // Every line carries a quieter half now; what must not leak is the *cpu's*
  // one -- a core count read as the size of a memory or of a storage pool.
  it("qualifies each line with its own measure, never with the cpu's", () => {
    render(<ClusterCard cluster={degradedCluster()} thresholds={evenly(0.8)} />);

    expect(screen.getByText("· 212 / 256 GiB").parentElement?.textContent).toBe(
      `83${NNBSP}%· 212 / 256 GiB`,
    );
    expect(screen.getByText("· 3,9 / 8 TiB").parentElement?.textContent).toBe(
      `49${NNBSP}%· 3,9 / 8 TiB`,
    );
    // And the core count stays where it measures something: the cpu line.
    expect(cpuRow().textContent).toBe(`CPU31${NNBSP}%· 72 c`);
    expect(screen.getAllByText(/72 c/)).toHaveLength(1);
  });
});

describe("node uptime in the list", () => {
  it("shows each node's uptime beside its name", () => {
    render(
      <ClusterCard
        cluster={healthyCluster({ nodes: [node("prox-qual-2201-cit")] })}
        thresholds={evenly(0.8)}
      />,
    );

    const row = screen.getByText("prox-qual-2201-cit").closest("tr");
    expect(row).not.toBeNull();
    expect(within(row as HTMLElement).getByText("41 j")).toBeInTheDocument();
  });

  it("shows an em dash for an offline node rather than claiming it just booted", () => {
    // An offline node has no uptime, and the payload says so with a null.
    const offline = node("prox-qual-2202-cit", "offline");
    expect(offline.uptime).toBeNull();
    render(<ClusterCard cluster={healthyCluster({ nodes: [offline] })} thresholds={evenly(0.8)} />);

    const row = screen.getByText("prox-qual-2202-cit").closest("tr");
    expect(within(row as HTMLElement).getByText("—")).toBeInTheDocument();
    expect(within(row as HTMLElement).queryByText(/0\s*s/)).not.toBeInTheDocument();
  });

  it("keeps the uptime of a node in maintenance, alongside its wrench", () => {
    // A drained node is still up: it refuses new guests, it did not restart.
    const drained = node("prox-pprd-2302-cit", "maintenance");
    render(<ClusterCard cluster={degradedCluster({ nodes: [drained] })} thresholds={evenly(0.8)} />);

    const row = screen.getByText("prox-pprd-2302-cit").closest("tr");
    expect(within(row as HTMLElement).getByText("41 j")).toBeInTheDocument();
    expect(
      within(row as HTMLElement).getByRole("img", { name: "Maintenance" }),
    ).toBeInTheDocument();
  });

  it("shows an em dash for an unknown node", () => {
    const ghost = node("prox-qual-2203-cit", "unknown");
    render(<ClusterCard cluster={healthyCluster({ nodes: [ghost] })} thresholds={evenly(0.8)} />);

    const row = screen.getByText("prox-qual-2203-cit").closest("tr");
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
        thresholds={evenly(0.8)}
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
      <ClusterCard cluster={degradedCluster()} usage={usage(0.2, 0.4)} thresholds={evenly(0.8)} />,
    );

    expect(screen.getByText(`31${NNBSP}%`, EXACT)).toBeInTheDocument();
    expect(screen.getByText(`83${NNBSP}%`, EXACT)).toBeInTheDocument();
    expect(screen.getByText("· 212 / 256 GiB")).toBeInTheDocument();
  });

  it("names both metrics in the chart's accessible label", () => {
    // Nothing is carried by colour alone: the curves are named in words, and
    // so is what they currently read.
    render(
      <ClusterCard cluster={degradedCluster()} usage={usage(0.2, 0.4)} thresholds={evenly(0.8)} />,
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
        thresholds={evenly(0.8)}
      />,
    );

    // Two runs for the cpu curve, one for the memory curve, which has the same
    // hole: four polylines would mean the gap was drawn through.
    expect(container.querySelectorAll("polyline")).toHaveLength(4);
  });

  it("says it has nothing to draw while the hour has not arrived", () => {
    // The card is served either way: a chart that could not be fetched costs
    // the curve, never the figures or the node list.
    render(<ClusterCard cluster={degradedCluster()} thresholds={evenly(0.8)} />);

    expect(
      screen.getByRole("img", { name: /utilisation.*aucune donnée/i }),
    ).toBeInTheDocument();
    expect(screen.getByText(`31${NNBSP}%`, EXACT)).toBeInTheDocument();
  });
});
