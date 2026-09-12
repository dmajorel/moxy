import {
  fireEvent,
  getDefaultNormalizer,
  render,
  screen,
} from "@testing-library/react";

import type { ClusterOverview } from "@/api/types";
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
    alerts: [{ kind: "memory_high", ratio: 0.828, nodes: ["a", "b"] }],
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
  };
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

  it("passes the threshold down, so memory above it turns amber and cpu does not", () => {
    render(<ClusterCard cluster={degradedCluster()} threshold={0.8} />);

    const memory = screen.getByRole("progressbar", { name: "Mémoire" });
    expect(memory.firstElementChild).toHaveClass("bg-warning");
    const cpu = screen.getByRole("progressbar", { name: "CPU" });
    expect(cpu.firstElementChild).toHaveClass("bg-accent");
  });

  it("honours a threshold raised above the current memory ratio", () => {
    render(<ClusterCard cluster={degradedCluster()} threshold={0.9} />);

    const memory = screen.getByRole("progressbar", { name: "Mémoire" });
    expect(memory.firstElementChild).toHaveClass("bg-accent");
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
      screen.getByText(`Mémoire à 83${NNBSP}% sur 2 nœuds`, EXACT),
    ).toBeInTheDocument();
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
        kind: "unreachable",
        message: "dial tcp 10.0.0.1:8006: connect: connection refused",
      },
    });
    const { container } = render(<ClusterCard cluster={cluster} threshold={0.8} />);

    expect(screen.getByText("Lecture ancienne · il y a 3 min")).toBeInTheDocument();
    expect(container.textContent).not.toContain("connection refused");
    expect(container.textContent).not.toContain("dial tcp");
  });

  it("reports that nothing was ever read when there is no reading at all", () => {
    const cluster = healthyCluster({
      status: "unreachable",
      fetchedAt: null,
      error: { kind: "unreachable", message: "context deadline exceeded" },
    });
    const { container } = render(<ClusterCard cluster={cluster} threshold={0.8} />);

    expect(screen.getByText("Aucune lecture disponible")).toBeInTheDocument();
    expect(container.textContent).not.toContain("context deadline exceeded");
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
