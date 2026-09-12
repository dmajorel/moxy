import type { Overview } from "@/api/types";

/**
 * Narrows an overview to a single cluster, recomputing the header figures so
 * they describe what is actually on screen.
 *
 * The totals mirror ComputeTotals in the backend, including its convention that
 * a node in maintenance still counts as online: it is reachable, it simply
 * refuses new guests.
 */
export function filterOverview(overview: Overview, clusterId: string | null): Overview {
  if (clusterId === null) {
    return overview;
  }

  const clusters = overview.clusters.filter((cluster) => cluster.id === clusterId);

  let nodes = 0;
  let nodesOnline = 0;
  let vms = 0;
  let alerts = 0;
  for (const cluster of clusters) {
    nodes += cluster.nodes.length;
    nodesOnline += cluster.nodes.filter(
      (node) => node.status === "online" || node.status === "maintenance",
    ).length;
    vms += cluster.vms.total;
    alerts += cluster.alerts.length;
  }

  return {
    ...overview,
    totals: { ...overview.totals, clusters: clusters.length, nodes, nodesOnline, vms, alerts },
    clusters,
  };
}
