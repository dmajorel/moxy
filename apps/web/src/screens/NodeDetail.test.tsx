import { fireEvent, render, screen, within } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import type {
  Guest,
  NodeDetail as NodeDetailData,
  Thresholds,
} from "@/api/types";

import { NodeDetail } from "./NodeDetail";

/**
 * One figure for the three resources, which is what the defaults are: a test
 * that needs them apart says so on the spot.
 */
function evenly(ratio: number): Thresholds {
  return { memory: ratio, cpu: ratio, storage: ratio };
}

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
    haState: null,
    // The node view never asks: unknown is what this route serves.
    agent: null,
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

/** The coloured part of a metric card's bar, which is where the cue lives. */
function fillOf(label: string): Element {
  const bar = screen.getByRole("progressbar", { name: label });
  const fill = bar.firstElementChild;
  if (fill === null) {
    throw new Error(`the bar of ${label} has no fill`);
  }
  return fill;
}

function renderNode(patch: Partial<NodeDetailData> = {}) {
  return render(
    <NodeDetail
      node={node(patch)}
      clusterName="Qualification"
      series={null}
      timeframe="hour"
      thresholds={evenly(0.8)}
    />,
  );
}

function renderNodeWithGuestLink(
  onSelectGuest: (vmid: number) => void,
  patch: Partial<NodeDetailData> = {},
) {
  return render(
    <NodeDetail
      node={node(patch)}
      clusterName="Qualification"
      series={null}
      timeframe="hour"
      thresholds={evenly(0.8)}
      onSelectGuest={onSelectGuest}
    />,
  );
}

describe("NodeDetail", () => {
  // A single threshold coloured the local disk by the memory limit. Each card
  // now reads its own, which is what lets an operator raise memory to 0,9
  // without raising the disk with it.
  it("colours each metric card by the threshold of its own resource", () => {
    render(
      <NodeDetail
        node={node({
          cpu: { ratio: 0.75, cores: 32 },
          memory: { used: 109 * GIB, total: 128 * GIB, ratio: 0.85 },
          rootfs: { used: 1350 * GIB, total: 1800 * GIB, ratio: 0.75 },
        })}
        clusterName="Qualification"
        series={null}
        timeframe="hour"
        thresholds={{ memory: 0.9, cpu: 0.8, storage: 0.7 }}
      />,
    );

    // 75 % of CPU under a 0,8 limit, 85 % of RAM under a 0,9 limit: neither
    // warns. 75 % of local disk over a 0,7 limit does.
    expect(fillOf("CPU")).toHaveClass("bg-accent");
    expect(fillOf("Mémoire")).toHaveClass("bg-accent");
    expect(fillOf("Stockage local")).toHaveClass("bg-warning");
  });

  it("puts the state beside the name, not in a list below", () => {
    renderNode();

    const heading = screen.getByRole("heading", { name: "prox-qual-2201-cit" });
    const header = heading.closest("header");
    expect(header).not.toBeNull();
    expect(within(header as HTMLElement).getByText(/En ligne/)).toBeInTheDocument();
    expect(within(header as HTMLElement).getByText("PVE 9.2.11")).toBeInTheDocument();
  });

  // The §2 asks for the state to be read first. "Hors ligne · 0 s" buried it
  // under a duration that claims the node rebooted this very second; the
  // payload now says null, and the line stops after the state.
  it("shows an offline node without a duration", () => {
    render(
      <NodeDetail
        node={node({ status: "offline", uptime: null })}
        clusterName="Qualification"
        series={null}
        timeframe="hour"
        thresholds={evenly(0.8)}
      />,
    );

    const header = screen
      .getByRole("heading", { name: "prox-qual-2201-cit" })
      .closest("header");
    expect(header).not.toBeNull();
    expect(within(header as HTMLElement).getByText("Hors ligne")).toBeInTheDocument();
    expect(within(header as HTMLElement).queryByText(/0\s*s/)).not.toBeInTheDocument();
    expect(within(header as HTMLElement).queryByText(/·\s*—/)).not.toBeInTheDocument();
  });

  // A series with no measured sample has no average. "moy. 0 %" announced an
  // idle node over a window nobody could read.
  it("shows an em dash for the average of a series with no reading", () => {
    render(
      <NodeDetail
        node={node()}
        clusterName="Qualification"
        series={{
          cluster: "qualification",
          timeframe: "hour",
          fetchedAt: "2026-09-12T12:00:00Z",
          points: [
            { time: "2026-09-12T11:00:00Z", cpu: null, memUsed: null, memTotal: null, netIn: null, netOut: null },
            { time: "2026-09-12T11:01:00Z", cpu: null, memUsed: null, memTotal: null, netIn: null, netOut: null },
          ],
          cpuAverage: null,
        }}
        timeframe="hour"
        thresholds={evenly(0.8)}
      />,
    );

    expect(screen.getByText("Dernière heure · moy. —")).toBeInTheDocument();
  });

  // "Invité" rather than "VM": a node hosts containers too, and counting them
  // as virtual machines contradicts every other tool the operator uses.
  it("counts guests apart from templates in the header chips", () => {
    renderNode();
    expect(screen.getByText("1 invité · 1 modèle")).toBeInTheDocument();
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
    expect(within(row as HTMLElement).getByText("Modèle")).toBeInTheDocument();
  });

  it("opens a guest when its name is activated", () => {
    const onSelectGuest = vi.fn();
    renderNodeWithGuestLink(onSelectGuest, {
      guests: [guest(103, { name: "sli-airflow-sep-exp-2601-qul" })],
    });

    // Named for what it does, the machine name kept so the visible label is
    // part of the accessible one.
    const open = screen.getByRole("button", {
      name: "Ouvrir sli-airflow-sep-exp-2601-qul",
    });
    fireEvent.click(open);

    expect(onSelectGuest).toHaveBeenCalledTimes(1);
    expect(onSelectGuest).toHaveBeenCalledWith(103);
  });

  it("leaves the names inert when no handler is given", () => {
    // A button leading nowhere would promise a navigation the caller cannot do.
    renderNode({ guests: [guest(103, { name: "sli-airflow-sep-exp-2601-qul" })] });

    expect(
      screen.queryByRole("button", { name: /^Ouvrir / }),
    ).not.toBeInTheDocument();
  });

  it("explains an empty guest list on a drained node", () => {
    renderNode({ status: "maintenance", guests: [] });

    expect(screen.getByText(/vidé par la mise en maintenance/i)).toBeInTheDocument();
    expect(screen.getByText(/migrés/i)).toBeInTheDocument();
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

  // A chart with no time axis does not say WHEN the spike it shows happened,
  // which is the first thing an operator asks of it.
  it("writes the time marks under the chart", () => {
    render(
      <NodeDetail
        node={node()}
        clusterName="Qualification"
        series={{
          cluster: "qualification",
          timeframe: "hour",
          fetchedAt: "2026-09-12T12:00:00Z",
          points: [
            { time: new Date(2026, 8, 12, 11, 0).toISOString(), cpu: 0.1, memUsed: null, memTotal: null, netIn: null, netOut: null },
            { time: new Date(2026, 8, 12, 11, 30).toISOString(), cpu: 0.2, memUsed: null, memTotal: null, netIn: null, netOut: null },
            { time: new Date(2026, 8, 12, 12, 0).toISOString(), cpu: 0.3, memUsed: null, memTotal: null, netIn: null, netOut: null },
          ],
          cpuAverage: 0.2,
        }}
        timeframe="hour"
        thresholds={evenly(0.8)}
      />,
    );

    expect(screen.getByText("11:00")).toBeInTheDocument();
    expect(screen.getByText("11:30")).toBeInTheDocument();
    expect(screen.getByText("12:00")).toBeInTheDocument();
  });

  // A spike seen this morning is not in the hour just gone; the five windows
  // of the RRD routes are what makes it findable.
  it("asks for another window when one is picked", () => {
    const onTimeframeChange = vi.fn();
    render(
      <NodeDetail
        node={node()}
        clusterName="Qualification"
        series={null}
        timeframe="hour"
        onTimeframeChange={onTimeframeChange}
        thresholds={evenly(0.8)}
      />,
    );

    fireEvent.click(screen.getByRole("radio", { name: "Dernières 24 h" }));

    expect(onTimeframeChange).toHaveBeenCalledWith("day");
  });

  // A control nobody listens to promises a choice the caller cannot honour.
  it("draws no picker without a handler", () => {
    renderNode();

    expect(screen.queryByRole("radiogroup")).not.toBeInTheDocument();
  });

  // The legend follows the window the PAYLOAD carries, not the button that is
  // lit: a reading fetched over the hour must not be captioned "7 derniers
  // jours" because the window was switched while it was in flight. Past the
  // day the marks carry a date, an hour saying nothing about where a sample
  // sits in a week.
  it("captions the window the series says it holds", () => {
    render(
      <NodeDetail
        node={node()}
        clusterName="Qualification"
        series={{
          cluster: "qualification",
          timeframe: "week",
          fetchedAt: "2026-09-12T12:00:00Z",
          points: [
            { time: new Date(2026, 8, 5, 12, 0).toISOString(), cpu: 0.1, memUsed: null, memTotal: null, netIn: null, netOut: null },
            { time: new Date(2026, 8, 8, 12, 0).toISOString(), cpu: 0.2, memUsed: null, memTotal: null, netIn: null, netOut: null },
            { time: new Date(2026, 8, 12, 12, 0).toISOString(), cpu: 0.3, memUsed: null, memTotal: null, netIn: null, netOut: null },
          ],
          cpuAverage: 0.2,
        }}
        timeframe="week"
        thresholds={evenly(0.8)}
      />,
    );

    expect(screen.getByText(/^7 derniers jours · moy\./)).toBeInTheDocument();
    expect(screen.getByText("05/09")).toBeInTheDocument();
    expect(screen.getByText("12/09")).toBeInTheDocument();
  });
});
