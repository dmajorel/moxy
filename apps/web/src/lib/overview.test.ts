import { describe, expect, it } from "vitest";

import type { ClusterOverview, Overview } from "@/api/types";

import { filterOverview } from "./overview";

function cluster(id: string, patch: Partial<ClusterOverview> = {}): ClusterOverview {
  return {
    id,
    name: id,
    color: null,
    status: "healthy",
    fetchedAt: null,
    error: null,
    quorum: null,
    cpu: { ratio: 0, cores: 0 },
    memory: { used: 0, total: 0, ratio: 0 },
    storage: { used: 0, total: 0, ratio: 0 },
    vms: { running: 0, stopped: 0, templates: 0, total: 0 },
    nodes: [],
    updates: null,
    alerts: [],
    ...patch,
  };
}

function node(name: string, status: ClusterOverview["nodes"][number]["status"]) {
  return {
    name,
    status,
    uptime: 0,
    cpu: { ratio: 0, cores: 0 },
    memory: { used: 0, total: 0, ratio: 0 },
    pendingUpdates: null,
    guests: [],
  };
}

const overview: Overview = {
  generatedAt: "2026-09-12T10:00:00Z",
  thresholds: { memory: 0.8, cpu: 0.8, storage: 0.8 },
  totals: { clusters: 2, nodes: 4, nodesOnline: 3, vms: 60, alerts: 2 },
  clusters: [
    cluster("qualification", {
      nodes: [node("q1", "online"), node("q2", "online")],
      vms: { running: 13, stopped: 0, templates: 1, total: 13 },
    }),
    cluster("preproduction", {
      status: "degraded",
      nodes: [node("p1", "online"), node("p2", "maintenance"), node("p3", "offline")],
      vms: { running: 44, stopped: 2, templates: 0, total: 46 },
      alerts: [{ kind: "memory_high" }, { kind: "node_offline" }],
    }),
  ],
};

describe("filterOverview", () => {
  it("returns the overview untouched when nothing is selected", () => {
    expect(filterOverview(overview, null)).toBe(overview);
  });

  it("keeps only the selected cluster", () => {
    const filtered = filterOverview(overview, "qualification");

    expect(filtered.clusters).toHaveLength(1);
    expect(filtered.clusters[0]?.id).toBe("qualification");
  });

  it("recomputes the totals so the header describes what is on screen", () => {
    const filtered = filterOverview(overview, "preproduction");

    expect(filtered.totals).toEqual({
      clusters: 1,
      nodes: 3,
      // A node in maintenance still counts as online; the offline one does not.
      nodesOnline: 2,
      vms: 46,
      alerts: 2,
    });
  });

  it("yields empty totals for an unknown cluster rather than throwing", () => {
    const filtered = filterOverview(overview, "does-not-exist");

    expect(filtered.clusters).toHaveLength(0);
    expect(filtered.totals.nodes).toBe(0);
    expect(filtered.totals.vms).toBe(0);
  });

  it("leaves the source overview untouched", () => {
    filterOverview(overview, "qualification");

    expect(overview.clusters).toHaveLength(2);
    expect(overview.totals.nodes).toBe(4);
  });
});
