import { describe, expect, it } from "vitest";

import type { ClusterOverview } from "@/api/types";

import {
  countMatches,
  filterClusters,
  firstMatch,
  formatMatchCount,
  normalizeQuery,
} from "./search";

const GIB = 1024 ** 3;

function guest(vmid: number, name: string) {
  return {
    vmid,
    name,
    kind: "qemu" as const,
    status: "running" as const,
    cpu: { ratio: 0.02, cores: 4 },
    memory: { used: 2 * GIB, total: 8 * GIB, ratio: 0.25 },
    tags: [],
  };
}

function node(name: string, guests: ReturnType<typeof guest>[] = []) {
  return {
    name,
    status: "online" as const,
    uptime: 3600,
    cpu: { ratio: 0.04, cores: 32 },
    memory: { used: 20 * GIB, total: 128 * GIB, ratio: 20 / 128 },
    pendingUpdates: null,
    pveVersion: null,
    guests,
  };
}

function cluster(id: string, name: string, nodes: ReturnType<typeof node>[]): ClusterOverview {
  return {
    id,
    name,
    color: null,
    status: "healthy",
    fetchedAt: "2026-09-12T10:00:00Z",
    error: null,
    quorum: null,
    cpu: null,
    memory: null,
    storage: { used: 1, total: 2, ratio: 0.5 },
    vms: { running: 0, stopped: 0, templates: 0, total: 0 },
    nodes,
    updates: null,
    alerts: [],
  };
}

const CLUSTERS: ClusterOverview[] = [
  cluster("qual", "Qualification", [
    node("prox-qual-2201-cit", [guest(100, "airflow-sep-exp")]),
    node("prox-qual-2202-cit"),
  ]),
  cluster("pprd", "Préproduction", [
    node("prox-pprd-2301-cit", [guest(1000, "keycloak-worker")]),
    node("prox-pprd-2302-cit", [guest(1002, "rabbitmq-api")]),
  ]),
];

describe("normalizeQuery", () => {
  // The estate names its clusters in French and its nodes in ASCII: a search
  // that distinguished the two would be wrong about half the tree.
  it("strips case and diacritics so prepro finds Préproduction", () => {
    expect(normalizeQuery("  PRÉprod  ")).toBe("preprod");
    expect(normalizeQuery("Préproduction")).toContain("preproduction");
  });
});

describe("filterClusters", () => {
  // The same array, not a copy: a keystroke that changes nothing must not
  // re-render the tree.
  it("returns the input untouched for an empty query", () => {
    expect(filterClusters(CLUSTERS, "")).toBe(CLUSTERS);
    expect(filterClusters(CLUSTERS, "   ")).toBe(CLUSTERS);
  });

  it("keeps the path down to a node that matches", () => {
    const shown = filterClusters(CLUSTERS, "2302");

    expect(shown).toHaveLength(1);
    expect(shown[0]?.id).toBe("pprd");
    expect(shown[0]?.nodes.map((n) => n.name)).toEqual(["prox-pprd-2302-cit"]);
  });

  // The node answers, so everything it holds is shown: that is what "open this
  // node" means.
  it("keeps every guest of a node that matches", () => {
    const shown = filterClusters(CLUSTERS, "2201");

    expect(shown[0]?.nodes[0]?.guests.map((g) => g.vmid)).toEqual([100]);
  });

  it("keeps the path down to a guest, and only that guest", () => {
    const shown = filterClusters(CLUSTERS, "rabbitmq");

    expect(shown).toHaveLength(1);
    expect(shown[0]?.nodes).toHaveLength(1);
    expect(shown[0]?.nodes[0]?.name).toBe("prox-pprd-2302-cit");
    expect(shown[0]?.nodes[0]?.guests.map((g) => g.name)).toEqual(["rabbitmq-api"]);
  });

  it("finds a guest by its vmid", () => {
    const shown = filterClusters(CLUSTERS, "1002");
    expect(shown[0]?.nodes[0]?.guests.map((g) => g.vmid)).toEqual([1002]);
  });

  // A cluster named in the query shows everything it holds: the operator asked
  // for the cluster, not for a subset of it.
  it("keeps a whole cluster that matches by name", () => {
    const shown = filterClusters(CLUSTERS, "prepro");

    expect(shown).toHaveLength(1);
    expect(shown[0]?.nodes).toHaveLength(2);
    expect(shown[0]?.nodes[0]?.guests).toHaveLength(1);
  });

  it("matches a cluster by its id as well as by its name", () => {
    expect(filterClusters(CLUSTERS, "qual")).toHaveLength(1);
  });

  it("keeps nothing when nothing answers", () => {
    expect(filterClusters(CLUSTERS, "zzzz")).toHaveLength(0);
  });
});

describe("countMatches", () => {
  // A cluster shown only because one of its guests answered is not a result,
  // it is the way to one.
  it("counts the rows that matched, not the ancestors shown with them", () => {
    expect(countMatches(CLUSTERS, "rabbitmq")).toBe(1);
    expect(countMatches(CLUSTERS, "2302")).toBe(1);
    expect(countMatches(CLUSTERS, "prox-pprd")).toBe(2);
    expect(countMatches(CLUSTERS, "")).toBe(0);
    expect(countMatches(CLUSTERS, "zzzz")).toBe(0);
  });
});

describe("firstMatch", () => {
  it("goes to the first result in tree order", () => {
    expect(firstMatch(CLUSTERS, "prox-pprd")).toEqual({
      kind: "node",
      clusterId: "pprd",
      node: "prox-pprd-2301-cit",
    });
    expect(firstMatch(CLUSTERS, "1002")).toEqual({
      kind: "guest",
      clusterId: "pprd",
      vmid: 1002,
    });
    expect(firstMatch(CLUSTERS, "qualif")).toEqual({
      kind: "cluster",
      clusterId: "qual",
    });
  });

  it("goes nowhere when nothing answers", () => {
    expect(firstMatch(CLUSTERS, "zzzz")).toBeNull();
    expect(firstMatch(CLUSTERS, "")).toBeNull();
  });
});

describe("formatMatchCount", () => {
  it("agrees in number", () => {
    expect(formatMatchCount(0)).toBe("Aucun résultat");
    expect(formatMatchCount(1)).toBe("1 résultat");
    expect(formatMatchCount(4)).toBe("4 résultats");
  });
});
