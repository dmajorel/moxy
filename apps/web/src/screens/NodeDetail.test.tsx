import { render, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import type { Guest, NodeDetail as NodeDetailData } from "@/api/types";

import { NodeDetail } from "./NodeDetail";

const GIB = 1024 ** 3;

function guest(vmid: number, patch: Partial<Guest> = {}): Guest {
  return {
    vmid,
    name: `sli-app-${String(vmid)}-26${String(vmid)}-qul`,
    kind: "qemu",
    status: "running",
    cpu: { ratio: 0.02, cores: 4 },
    memory: { used: 2 * GIB, total: 8 * GIB, ratio: 0.25 },
    tags: [],
    ...patch,
  };
}

function node(patch: Partial<NodeDetailData> = {}): NodeDetailData {
  return {
    cluster: "qualification",
    name: "prox-qual-2201-cit",
    status: "online",
    uptime: 3_542_400,
    fetchedAt: "2026-09-12T12:00:00Z",
    pveVersion: "9.2.11",
    kernelVersion: "6.14.8-2-pve",
    cpu: { ratio: 0.031, cores: 32 },
    memory: { used: 18 * GIB, total: 128 * GIB, ratio: 0.140625 },
    swap: { used: 0, total: 8 * GIB, ratio: 0 },
    rootfs: { used: 412 * GIB, total: 1800 * GIB, ratio: 0.2289 },
    loadAverage: [0.84, 0.91, 0.88],
    quorum: { quorate: true, nodes: 3, online: 3 },
    haState: "actif",
    pendingUpdates: 0,
    updates: [],
    guests: [guest(100), guest(101, { status: "template" })],
    ...patch,
  };
}

function renderNode(patch: Partial<NodeDetailData> = {}) {
  return render(
    <NodeDetail
      node={node(patch)}
      clusterName="Qualification"
      series={null}
      threshold={0.8}
    />,
  );
}

describe("NodeDetail", () => {
  it("puts the state beside the name, not in a list below", () => {
    renderNode();

    const heading = screen.getByRole("heading", { name: "prox-qual-2201-cit" });
    const header = heading.closest("header");
    expect(header).not.toBeNull();
    expect(within(header as HTMLElement).getByText(/En ligne/)).toBeInTheDocument();
    expect(within(header as HTMLElement).getByText("PVE 9.2.11")).toBeInTheDocument();
  });

  it("counts guests apart from templates in the header chips", () => {
    renderNode();
    expect(screen.getByText("1 VM · 1 template")).toBeInTheDocument();
  });

  it("shows the load average as three figures", () => {
    renderNode();
    expect(screen.getByText("0,84 · 0,91 · 0,88")).toBeInTheDocument();
  });

  it("renders an em dash rather than a zero when the load is unknown", () => {
    // null means the node did not report it; 0,00 would assert it is idle.
    renderNode({ loadAverage: null, guests: [] });

    const card = screen.getByText("Load average").closest("div");
    expect(card).not.toBeNull();
    expect(within(card as HTMLElement).getByText("—")).toBeInTheDocument();
  });

  it("says a standalone node has no quorum instead of inventing one", () => {
    renderNode({ quorum: null });
    expect(screen.getByText("Nœud seul")).toBeInTheDocument();
  });

  it("reports quorum loss", () => {
    renderNode({ quorum: { quorate: false, nodes: 3, online: 1 } });
    expect(screen.getByText("Perdu · 1/3 votes")).toBeInTheDocument();
  });

  it("names a guest exactly as PVE does, the id staying in its own column", () => {
    renderNode({ guests: [guest(103, { name: "sli-airflow-sep-exp-2601-qul" })] });

    // Same string as the tree shows: copyable, searchable, not a rewrite.
    const cell = screen.getByText("sli-airflow-sep-exp-2601-qul");
    expect(cell.tagName).toBe("TD");
    // The vmid belongs to the ID column and must not be repeated beside it.
    expect(cell.textContent).not.toContain("103");
    const cells = within(cell.closest("tr") as HTMLElement).getAllByRole("cell");
    expect(cells[0]?.textContent).toBe("103");
    expect(cells[1]?.textContent).toBe("sli-airflow-sep-exp-2601-qul");
  });

  it("leaves a template's runtime figures blank", () => {
    // A template consumes nothing; a CPU of 0 % would suggest it is merely idle.
    renderNode({ guests: [guest(101, { status: "template" })] });

    const row = screen.getByText("sli-app-101-26101-qul").closest("tr");
    expect(row).not.toBeNull();
    expect(within(row as HTMLElement).getAllByText("—")).toHaveLength(2);
    expect(within(row as HTMLElement).getByText("template")).toBeInTheDocument();
  });

  it("explains an empty guest list on a drained node", () => {
    renderNode({ status: "maintenance", guests: [] });

    expect(screen.getByText(/vidé par la mise en maintenance/i)).toBeInTheDocument();
    expect(screen.getByText(/migrées/i)).toBeInTheDocument();
  });

  it("reports unknown pending updates as unknown, not as up to date", () => {
    renderNode({ pendingUpdates: null, updates: null });

    const updates = screen.getByText("Mises à jour").closest("div");
    expect(updates).not.toBeNull();
    expect(within(updates as HTMLElement).getByText("—")).toBeInTheDocument();
    expect(
      screen.queryByRole("heading", { name: "Mises à jour en attente" }),
    ).not.toBeInTheDocument();
  });

  it("lists the pending packages, with the versions on either side", () => {
    renderNode({
      pendingUpdates: 2,
      updates: [
        {
          package: "proxmox-firewall",
          title: "Proxmox nftables firewall implementation",
          oldVersion: null,
          version: "1.2.0",
        },
        {
          package: "pve-manager",
          title: "Proxmox Virtual Environment Management Tools",
          oldVersion: "9.2.11",
          version: "9.2.12",
        },
      ],
    });

    expect(
      screen.getByRole("heading", { name: "Mises à jour en attente" }),
    ).toBeInTheDocument();

    const row = screen.getByText("pve-manager").closest("tr");
    expect(row).not.toBeNull();
    expect(within(row as HTMLElement).getByText("9.2.11 → 9.2.12")).toBeInTheDocument();
    expect(
      within(row as HTMLElement).getByText(
        "Proxmox Virtual Environment Management Tools",
      ),
    ).toBeInTheDocument();

    // A package apt would add reports no installed version: the cell shows the
    // new one alone rather than an arrow starting from nothing.
    const added = screen.getByText("proxmox-firewall").closest("tr");
    expect(added).not.toBeNull();
    expect(within(added as HTMLElement).getByText("1.2.0")).toBeInTheDocument();
  });

  it("says nothing more than 'À jour' when no package is pending", () => {
    // An empty table would repeat, badly, what the summary row already says.
    renderNode({ pendingUpdates: 0, updates: [] });

    expect(screen.getByText("À jour")).toBeInTheDocument();
    expect(
      screen.queryByRole("heading", { name: "Mises à jour en attente" }),
    ).not.toBeInTheDocument();
  });
});
