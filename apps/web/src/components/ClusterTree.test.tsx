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

function makeGuest(vmid: number, name: string, status: GuestStatus = "running"): Guest {
  return { vmid, name, kind: "qemu", status, cpu: CPU, memory: USAGE, tags: [] };
}

function makeNode(name: string, status: NodeStatus, guests: Guest[] = []): Node {
  return {
    name,
    status,
    uptime: 3600,
    cpu: CPU,
    memory: USAGE,
    pendingUpdates: 0,
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
) {
  return render(
    <ClusterTree clusters={clusters} selection={selection} onSelect={onSelect} />,
  );
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

  it("marks a node in maintenance with a wrench besides its amber dot", () => {
    renderTree([preproduction()], { kind: "cluster", clusterId: "pprd" });

    const row = rowOf("prox-pprd-2302-cit");
    expect(within(row).getByRole("img", { name: "Maintenance planifiée" })).toBeInTheDocument();
    expect(within(row).getByRole("img", { name: "Maintenance" })).toBeInTheDocument();

    const healthy = rowOf("prox-pprd-2301-cit");
    expect(
      within(healthy).queryByRole("img", { name: "Maintenance planifiée" }),
    ).toBeNull();
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

  it("renders nothing but the footer when there is no cluster at all", () => {
    const { container } = renderTree([]);

    expect(container.querySelectorAll('[role="treeitem"]')).toHaveLength(0);
    expect(screen.getByRole("button", { name: "Ajouter un cluster" })).toBeDisabled();
  });

  it("offers an inert « Ajouter un cluster » entry that says why", () => {
    renderTree([qualification()]);

    const button = screen.getByRole("button", { name: "Ajouter un cluster" });
    expect(button).toBeDisabled();
    expect(button).toHaveClass("text-text-muted");
    expect(button).toHaveAttribute(
      "title",
      "L'ajout d'un cluster n'est pas encore disponible dans cette version",
    );
  });
});
