import { fireEvent, render, screen, within } from "@testing-library/react";

import type {
  ClusterOverview,
  Guest,
  GuestStatus,
  Node,
  NodeStatus,
} from "@/api/types";
import { ClusterTree } from "./ClusterTree";
import type { TreeSelection } from "./ClusterTree";

const CPU = { ratio: 0.03, cores: 32 };
const USAGE = { used: 1, total: 2, ratio: 0.5 };

function makeGuest(
  vmid: number,
  name: string,
  status: GuestStatus = "running",
  patch: Partial<Guest> = {},
): Guest {
  return {
    vmid,
    name,
    kind: "qemu",
    status,
    cpu: CPU,
    memory: USAGE,
    tags: [],
    haState: null,
    agent: null,
    ...patch,
  };
}

function makeNode(name: string, status: NodeStatus, guests: Guest[] = []): Node {
  return {
    name,
    status,
    uptime: 3600,
    cpu: CPU,
    memory: USAGE,
    pendingUpdates: 0,
    pveVersion: null,
    guests,
  };
}

function makeCluster(id: string, name: string, nodes: Node[]): ClusterOverview {
  return {
    id,
    name,
    color: null,
    status: "healthy",
    fetchedAt: "2026-09-12T10:00:00Z",
    error: null,
    quorum: null,
    cpu: CPU,
    memory: USAGE,
    storage: USAGE,
    vms: { running: 3, stopped: 0, templates: 1, total: 3 },
    nodes,
    updates: null,
    alerts: [],
  };
}

const GUESTS = [
  makeGuest(100, "sli-testproxmox-qul"),
  makeGuest(103, "sli-airflow-sep-exp-2601-qul"),
  makeGuest(101, "template-rocky10", "template"),
];

/** Qualification: three nodes, all up, the first one holding the guests. */
function qualification(): ClusterOverview {
  return makeCluster("qual", "Qualification", [
    makeNode("prox-qual-2201-cit", "online", GUESTS),
    makeNode("prox-qual-2202-cit", "online"),
    makeNode("prox-qual-2203-cit", "online"),
  ]);
}

/** Preproduction: one node drained for maintenance, which still counts as up. */
function preproduction(): ClusterOverview {
  return makeCluster("pprd", "Préproduction", [
    makeNode("prox-pprd-2301-cit", "online"),
    makeNode("prox-pprd-2302-cit", "maintenance"),
    makeNode("prox-pprd-2303-cit", "online"),
  ]);
}

/** Production: one node genuinely offline. */
function production(): ClusterOverview {
  return makeCluster("prod", "Production", [
    makeNode("prox-prod-2401-cit", "online"),
    makeNode("prox-prod-2402-cit", "offline"),
    makeNode("prox-prod-2403-cit", "online"),
  ]);
}

function renderTree(
  clusters: ClusterOverview[],
  selection: TreeSelection = { kind: "all" },
  onSelect: (next: TreeSelection) => void = () => {},
  query = "",
) {
  return render(
    <ClusterTree
      clusters={clusters}
      selection={selection}
      onSelect={onSelect}
      query={query}
    />,
  );
}

/** The labels of every row the tree currently shows, in order. */
function visibleRows(): string[] {
  return screen.getAllByRole("treeitem").map((row) => row.textContent ?? "");
}

function rowOf(label: string): HTMLElement {
  const element = screen.getByText(label).closest('[role="treeitem"]');
  if (element === null) {
    throw new Error(`no treeitem holds the label ${label}`);
  }
  return element as HTMLElement;
}

describe("ClusterTree", () => {
  it("renders the three levels of the hierarchy", () => {
    renderTree([qualification()], {
      kind: "node",
      clusterId: "qual",
      node: "prox-qual-2201-cit",
    });

    expect(screen.getByText("Qualification")).toBeInTheDocument();
    expect(screen.getByText("prox-qual-2201-cit")).toBeInTheDocument();
    expect(screen.getByText("sli-testproxmox-qul")).toBeInTheDocument();
    expect(rowOf("Qualification")).toHaveAttribute("aria-level", "1");
    expect(rowOf("prox-qual-2201-cit")).toHaveAttribute("aria-level", "2");
    expect(rowOf("sli-testproxmox-qul")).toHaveAttribute("aria-level", "3");
  });

  it("marks a cluster row with the accent it was configured with", () => {
    const cluster = qualification();
    cluster.color = "#7C5CD6";
    renderTree([cluster, preproduction()]);

    const mark = rowOf("Qualification").querySelector<HTMLElement>("[style]");
    expect(mark).not.toBeNull();
    expect(mark?.style.getPropertyValue("--cluster-accent")).toBe("#7C5CD6");
    expect(mark).toHaveAttribute("aria-hidden", "true");
    // The cluster next to it declared none, and gets none.
    expect(rowOf("Préproduction").querySelector("[style]")).toBeNull();
  });

  // The state of a cluster is the one thing on its row that nothing else
  // shows: the counter next to it counts nodes that answer, which is not the
  // same question — a cluster that lost its quorum still counts 3/3.
  it("paints the cluster glyph with the colour of its state", () => {
    const degraded = preproduction();
    degraded.status = "degraded";
    const unreachable = production();
    unreachable.status = "unreachable";
    renderTree([qualification(), degraded, unreachable]);

    expect(
      within(rowOf("Qualification")).getByRole("img", { name: "Sain" }),
    ).toHaveClass("text-text-success");
    expect(
      within(rowOf("Préproduction")).getByRole("img", { name: "Dégradé" }),
    ).toHaveClass("text-text-warning-strong");
    expect(
      within(rowOf("Production")).getByRole("img", { name: "Injoignable" }),
    ).toHaveClass("text-text-muted");
  });

  // Nothing is carried by colour alone — and nothing is said twice either:
  // the glyph replaced the silent copy of the state that sat next to the name.
  it("says the state of a cluster once, on the glyph that paints it", () => {
    renderTree([qualification()]);

    const said = within(rowOf("Qualification")).getAllByText("Sain");
    expect(said).toHaveLength(1);
    expect(said[0]?.tagName.toLowerCase()).toBe("title");
  });

  it("counts every node online as a success counter", () => {
    renderTree([qualification()]);

    const counter = screen.getByText("3/3");
    expect(counter).toHaveClass("bg-bg-success");
    expect(counter).toHaveClass("text-text-success");
  });

  it("counts a node in maintenance as online, like the backend totals do", () => {
    renderTree([preproduction()]);

    const counter = screen.getByText("3/3");
    expect(counter).toHaveClass("bg-bg-success");
  });

  it("turns the counter amber as soon as a node is down", () => {
    renderTree([production()]);

    const counter = screen.getByText("2/3");
    expect(counter).toHaveClass("bg-bg-warning");
    expect(counter).toHaveClass("text-text-warning");
  });

  // One fact, one named image. The row used to carry a dot called
  // "Maintenance" and, at the far end of the line, a wrench called
  // "Maintenance planifiée": a screen reader read both for a single fact.
  it("names a node in maintenance once, on the glyph that carries the wrench", () => {
    renderTree([preproduction()], { kind: "cluster", clusterId: "pprd" });

    const row = rowOf("prox-pprd-2302-cit");
    const images = within(row).getAllByRole("img");
    expect(images).toHaveLength(1);
    expect(images[0]).toHaveAccessibleName("Maintenance planifiée");

    const healthy = rowOf("prox-pprd-2301-cit");
    expect(
      within(healthy).queryByRole("img", { name: "Maintenance planifiée" }),
    ).toBeNull();
    expect(within(healthy).getAllByRole("img")[0]).toHaveAccessibleName("En ligne");
  });

  it("paints the node glyph with the colour of its state", () => {
    const nodes = [
      makeNode("n-online", "online"),
      makeNode("n-drained", "maintenance"),
      makeNode("n-offline", "offline"),
      makeNode("n-unknown", "unknown"),
    ];
    renderTree([makeCluster("qual", "Qualification", nodes)], {
      kind: "cluster",
      clusterId: "qual",
    });

    const glyphOf = (name: string) => within(rowOf(name)).getAllByRole("img")[0];
    expect(glyphOf("n-online")).toHaveClass("text-text-success");
    expect(glyphOf("n-drained")).toHaveClass("text-text-warning-strong");
    expect(glyphOf("n-offline")).toHaveClass("text-text-muted");
    expect(glyphOf("n-unknown")).toHaveClass("text-text-muted");
  });

  // The wrench REPLACES the server rather than sitting on it: a 9px badge in
  // the corner of a glyph this size is a smudge, not a tool.
  it("swaps the server for the wrench while a node is drained", () => {
    const nodes = [makeNode("n-drained", "maintenance"), makeNode("n-online", "online")];
    renderTree([makeCluster("qual", "Qualification", nodes)], {
      kind: "cluster",
      clusterId: "qual",
    });

    const drained = within(rowOf("n-drained")).getAllByRole("img")[0];
    expect(drained).toHaveClass("tabler-icon-tool");
    expect(drained).not.toHaveClass("tabler-icon-server");

    const healthy = within(rowOf("n-online")).getAllByRole("img")[0];
    expect(healthy).toHaveClass("tabler-icon-server");

    // Nothing of the overlay is left: no second glyph, no punched hole.
    const row = rowOf("n-drained");
    expect(within(row).getAllByRole("img")).toHaveLength(1);
    expect(row.querySelector("[class*='mask-image']")).toBeNull();
  });

  it("shows the full guest name and never its vmid", () => {
    renderTree([qualification()], {
      kind: "node",
      clusterId: "qual",
      node: "prox-qual-2201-cit",
    });

    const row = rowOf("sli-airflow-sep-exp-2601-qul");
    expect(row).toBeInTheDocument();
    expect(row.textContent).not.toContain("103");
    expect(screen.queryByText("103 · airflow-sep-exp")).toBeNull();
  });

  // The guest glyph replaces the 7px dot, so it must carry everything the dot
  // carried — the state, in colour and in words — plus the two things the dot
  // could not say at all.
  it("paints the guest glyph with the colour of its state", () => {
    const guests = [
      makeGuest(101, "vm-running"),
      makeGuest(102, "vm-stopped", "stopped"),
      makeGuest(103, "vm-troubled", "running", { haState: "error" }),
      makeGuest(104, "vm-agentless", "running", { agent: false }),
      makeGuest(9000, "vm-template", "template"),
    ];
    renderTree([makeCluster("qual", "Qualification", [makeNode("n1", "online", guests)])], {
      kind: "node",
      clusterId: "qual",
      node: "n1",
    });

    const glyphOf = (name: string) => within(rowOf(name)).getAllByRole("img")[0];
    expect(glyphOf("vm-running")).toHaveClass("text-text-success");
    expect(glyphOf("vm-stopped")).toHaveClass("text-text-muted");
    expect(glyphOf("vm-troubled")).toHaveClass("text-text-danger");
    expect(glyphOf("vm-agentless")).toHaveClass("text-text-info");
    expect(glyphOf("vm-template")).toHaveClass("text-text-muted");
  });

  // Colour alone says nothing to a screen reader, and red against green is
  // precisely the pair a deuteranope cannot separate. The glyph is therefore a
  // named image in all five states.
  it("names the state of every guest in words", () => {
    const guests = [
      makeGuest(101, "vm-running"),
      makeGuest(102, "vm-stopped", "stopped"),
      // The incident names the CRM's own word for it: "fault" alone would
      // leave the reader to open the page to learn which.
      makeGuest(103, "vm-troubled", "running", { haState: "fence" }),
      makeGuest(104, "vm-agentless", "running", { agent: false }),
      makeGuest(9000, "vm-template", "template"),
    ];
    renderTree([makeCluster("qual", "Qualification", [makeNode("n1", "online", guests)])], {
      kind: "node",
      clusterId: "qual",
      node: "n1",
    });

    const nameOf = (name: string) => within(rowOf(name)).getAllByRole("img")[0];
    expect(nameOf("vm-running")).toHaveAccessibleName("En cours");
    expect(nameOf("vm-stopped")).toHaveAccessibleName("Arrêtée");
    expect(nameOf("vm-troubled")).toHaveAccessibleName("Anomalie · Isolation");
    expect(nameOf("vm-agentless")).toHaveAccessibleName("Sans agent QEMU");
    expect(nameOf("vm-template")).toHaveAccessibleName("Modèle");
  });

  // An unknown agent is not a missing one: a VM the sweep has not reached must
  // look exactly like any other running VM.
  it("leaves a guest with an unknown agent on its runtime state", () => {
    const guests = [makeGuest(101, "vm-unswept", "running", { agent: null })];
    renderTree([makeCluster("qual", "Qualification", [makeNode("n1", "online", guests)])], {
      kind: "node",
      clusterId: "qual",
      node: "n1",
    });

    const glyph = within(rowOf("vm-unswept")).getAllByRole("img")[0];
    expect(glyph).toHaveClass("text-text-success");
    expect(glyph).toHaveAccessibleName("En cours");
  });

  it("renders a template with its icon and no status dot", () => {
    renderTree([qualification()], {
      kind: "node",
      clusterId: "qual",
      node: "prox-qual-2201-cit",
    });

    const template = rowOf("template-rocky10");
    const icons = within(template).getAllByRole("img");
    expect(icons).toHaveLength(1);
    expect(icons[0]).toHaveAccessibleName("Modèle");

    const running = rowOf("sli-testproxmox-qul");
    expect(within(running).getByRole("img", { name: "En cours" })).toBeInTheDocument();
  });

  it("emits the cluster variant when a cluster is clicked", () => {
    const onSelect = vi.fn();
    renderTree([qualification()], { kind: "all" }, onSelect);

    fireEvent.click(screen.getByText("Qualification"));

    expect(onSelect).toHaveBeenCalledWith({ kind: "cluster", clusterId: "qual" });
  });

  it("emits the node variant when a node is clicked", () => {
    const onSelect = vi.fn();
    renderTree([qualification()], { kind: "cluster", clusterId: "qual" }, onSelect);

    fireEvent.click(screen.getByText("prox-qual-2202-cit"));

    expect(onSelect).toHaveBeenCalledWith({
      kind: "node",
      clusterId: "qual",
      node: "prox-qual-2202-cit",
    });
  });

  it("emits the guest variant when a guest is clicked", () => {
    const onSelect = vi.fn();
    renderTree(
      [qualification()],
      { kind: "node", clusterId: "qual", node: "prox-qual-2201-cit" },
      onSelect,
    );

    fireEvent.click(screen.getByText("sli-airflow-sep-exp-2601-qul"));

    expect(onSelect).toHaveBeenCalledWith({
      kind: "guest",
      clusterId: "qual",
      node: "prox-qual-2201-cit",
      vmid: 103,
    });
  });

  it("opens the cluster and the node holding the selection", () => {
    renderTree([qualification()], {
      kind: "guest",
      clusterId: "qual",
      node: "prox-qual-2201-cit",
      vmid: 103,
    });

    expect(rowOf("Qualification")).toHaveAttribute("aria-expanded", "true");
    expect(rowOf("prox-qual-2201-cit")).toHaveAttribute("aria-expanded", "true");
    expect(rowOf("sli-airflow-sep-exp-2601-qul")).toHaveAttribute("aria-selected", "true");
  });

  it("collapses and expands a cluster from its chevron", () => {
    const { container } = renderTree([qualification()], {
      kind: "cluster",
      clusterId: "qual",
    });
    const chevron = within(rowOf("Qualification")).getByRole("presentation");

    fireEvent.click(chevron);

    expect(screen.queryByText("prox-qual-2201-cit")).toBeNull();
    expect(rowOf("Qualification")).toHaveAttribute("aria-expanded", "false");
    expect(container.querySelectorAll('[role="treeitem"]')).toHaveLength(1);

    fireEvent.click(within(rowOf("Qualification")).getByRole("presentation"));

    expect(screen.getByText("prox-qual-2201-cit")).toBeInTheDocument();
  });

  it("keeps a single tab stop and moves it with the arrow keys", () => {
    renderTree([qualification(), preproduction()], {
      kind: "cluster",
      clusterId: "qual",
    });

    const stops = screen
      .getAllByRole("treeitem")
      .filter((row) => row.getAttribute("tabindex") === "0");
    expect(stops).toHaveLength(1);
    expect(stops[0]).toBe(rowOf("Qualification"));

    const first = rowOf("Qualification");
    first.focus();
    fireEvent.keyDown(first, { key: "ArrowDown" });
    expect(rowOf("prox-qual-2201-cit")).toHaveFocus();

    fireEvent.keyDown(rowOf("prox-qual-2201-cit"), { key: "ArrowDown" });
    // The node is collapsed, so the next visible row is its sibling, never a guest.
    expect(rowOf("prox-qual-2202-cit")).toHaveFocus();

    fireEvent.keyDown(rowOf("prox-qual-2202-cit"), { key: "ArrowUp" });
    expect(rowOf("prox-qual-2201-cit")).toHaveFocus();
  });

  it("opens with ArrowRight, walks into the children, and closes with ArrowLeft", () => {
    renderTree([qualification()], { kind: "cluster", clusterId: "qual" });

    const node = rowOf("prox-qual-2201-cit");
    node.focus();
    fireEvent.keyDown(node, { key: "ArrowRight" });
    expect(rowOf("prox-qual-2201-cit")).toHaveAttribute("aria-expanded", "true");
    expect(screen.getByText("sli-testproxmox-qul")).toBeInTheDocument();

    fireEvent.keyDown(rowOf("prox-qual-2201-cit"), { key: "ArrowRight" });
    expect(rowOf("sli-testproxmox-qul")).toHaveFocus();

    fireEvent.keyDown(rowOf("sli-testproxmox-qul"), { key: "ArrowLeft" });
    expect(rowOf("prox-qual-2201-cit")).toHaveFocus();

    fireEvent.keyDown(rowOf("prox-qual-2201-cit"), { key: "ArrowLeft" });
    expect(screen.queryByText("sli-testproxmox-qul")).toBeNull();
  });

  it("selects with Enter and with Space", () => {
    const onSelect = vi.fn();
    renderTree([qualification()], { kind: "cluster", clusterId: "qual" }, onSelect);

    const row = rowOf("Qualification");
    row.focus();
    fireEvent.keyDown(row, { key: "Enter" });
    fireEvent.keyDown(row, { key: " " });

    expect(onSelect).toHaveBeenCalledTimes(2);
    expect(onSelect).toHaveBeenLastCalledWith({ kind: "cluster", clusterId: "qual" });
  });

  it("exposes the tree ARIA contract", () => {
    renderTree([qualification(), preproduction()], {
      kind: "node",
      clusterId: "qual",
      node: "prox-qual-2201-cit",
    });

    expect(
      screen.getByRole("tree", { name: "Arborescence des clusters" }),
    ).toBeInTheDocument();

    const cluster = rowOf("Qualification");
    expect(cluster).toHaveAttribute("aria-level", "1");
    expect(cluster).toHaveAttribute("aria-posinset", "1");
    expect(cluster).toHaveAttribute("aria-setsize", "2");
    expect(cluster).toHaveAttribute("aria-selected", "false");

    const selected = rowOf("prox-qual-2201-cit");
    expect(selected).toHaveAttribute("aria-selected", "true");
    expect(selected).toHaveClass("bg-bg-accent");
    expect(selected).toHaveClass("text-text-accent");

    // A leaf carries no aria-expanded at all: it has nothing to expand.
    expect(rowOf("sli-testproxmox-qul")).not.toHaveAttribute("aria-expanded");
    expect(rowOf("prox-qual-2202-cit")).not.toHaveAttribute("aria-expanded");
  });

  it("renders a cluster with no node and a node with no guest", () => {
    const empty = makeCluster("dr", "Reprise", []);
    empty.status = "unreachable";
    const lonely = makeCluster("lab", "Laboratoire", [makeNode("lab-1", "online")]);

    renderTree([empty, lonely], { kind: "cluster", clusterId: "lab" });

    expect(screen.getByText("Reprise")).toBeInTheDocument();
    expect(rowOf("Reprise")).not.toHaveAttribute("aria-expanded");
    expect(screen.getByText("0/0")).toHaveClass("bg-bg-warning");
    expect(rowOf("lab-1")).not.toHaveAttribute("aria-expanded");
  });

  it("says the tree is empty when there is no cluster at all", () => {
    const { container } = renderTree([]);

    expect(container.querySelectorAll('[role="treeitem"]')).toHaveLength(0);
    expect(container.querySelectorAll("button")).toHaveLength(0);

    // Outside the tree: a tree holds treeitem and group, not free text.
    const empty = screen.getByText("Aucun cluster configuré");
    expect(empty).toBeInTheDocument();
    expect(empty.closest('[role="tree"]')).toBeNull();
  });

  // A cluster is declared server-side; the tree must not suggest otherwise.
  it("offers no way to add a cluster", () => {
    renderTree([qualification()]);

    expect(screen.queryByRole("button", { name: /Ajouter un cluster/ })).toBeNull();
    expect(screen.queryByText("Aucun cluster configuré")).toBeNull();
  });
});

// The ⌘K field used to carry its query up to App and be read by nothing at
// all: an operator typed "pprd-2302", nothing happened, and concluded the tool
// was broken.
describe("the global search filters the tree", () => {
  const estate = [qualification(), preproduction(), production()];

  it("shows everything when the query is empty", () => {
    renderTree(estate, { kind: "all" }, () => {}, "");

    expect(visibleRows()).toHaveLength(3);
  });

  it("keeps the path down to a node, expanded", () => {
    renderTree(estate, { kind: "all" }, () => {}, "2302");

    const rows = visibleRows();
    expect(rows.join(" ")).toContain("Préproduction");
    expect(rows.join(" ")).toContain("prox-pprd-2302-cit");
    // The two clusters that answer nothing are gone, and so are the sibling
    // nodes of the one that does.
    expect(rows.join(" ")).not.toContain("Qualification");
    expect(rows.join(" ")).not.toContain("prox-pprd-2301-cit");
  });

  // A result buried in a collapsed branch is a result nobody sees, so the
  // filter opens what it kept rather than leaving it to the operator.
  it("expands what it kept, without being asked", () => {
    renderTree(estate, { kind: "all" }, () => {}, "airflow");

    const rows = visibleRows().join(" ");
    expect(rows).toContain("Qualification");
    expect(rows).toContain("prox-qual-2201-cit");
    expect(rows).toContain("airflow-sep-exp");
  });

  it("finds a guest by its vmid", () => {
    renderTree(estate, { kind: "all" }, () => {}, String(GUESTS[0]?.vmid ?? 0));

    expect(visibleRows().join(" ")).toContain(GUESTS[0]?.name ?? "");
  });

  // "Préproduction" has to be findable by typing "prepro": the estate names
  // its clusters in French and its nodes in ASCII.
  it("ignores case and diacritics", () => {
    renderTree(estate, { kind: "all" }, () => {}, "PREPRO");

    expect(visibleRows().join(" ")).toContain("Préproduction");
  });

  // A filter that silently empties a list leaves a screen reader with no way
  // to know why.
  it("announces how many rows answered", () => {
    const { rerender } = renderTree(estate, { kind: "all" }, () => {}, "2302");

    expect(screen.getByText("1 résultat")).toBeInTheDocument();

    rerender(
      <ClusterTree
        clusters={estate}
        selection={{ kind: "all" }}
        onSelect={() => {}}
        query="zzzz"
      />,
    );
    expect(screen.getByText("Aucun résultat")).toBeInTheDocument();
    expect(screen.queryAllByRole("treeitem")).toHaveLength(0);
  });

  // "Aucun cluster" is what an empty estate says. A query that matched nothing
  // is a different statement, and saying both would be saying neither.
  it("does not claim the estate is empty when a query matched nothing", () => {
    renderTree(estate, { kind: "all" }, () => {}, "zzzz");

    expect(screen.queryByText(/Aucun cluster/)).toBeNull();
  });
});
