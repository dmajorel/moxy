/**
 * Filtering the tree from the global search field.
 *
 * The ⌘K field of the handoff used to carry its query up to App and be read by
 * nothing at all: an operator typed "pprd-2302", nothing happened, and
 * concluded the tool was broken. This is what makes it do the thing it draws.
 *
 * The rule of what survives a query has to be stated once, because it is not
 * obvious: a row is kept when IT matches, when an ANCESTOR matches — a cluster
 * named in the query shows everything it holds — or when a DESCENDANT matches,
 * which is what keeps the path to a result visible. Anything else would show a
 * guest with no node above it, or a node with no cluster.
 */
import type { ClusterOverview, Guest, Node } from "@/api/types";
import type { Route } from "@/lib/routes";

/**
 * The comparison form of a piece of text: lowercased, and with its diacritics
 * removed.
 *
 * "Préproduction" must be found by typing "prepro". The estate this was
 * written for names its clusters in French and its nodes in ASCII, so a search
 * that distinguished the two would be wrong about half the tree.
 */
export function normalizeQuery(value: string): string {
  return value
    .normalize("NFD")
    .replace(/[̀-ͯ]/g, "")
    .toLowerCase()
    .trim();
}

/** Whether one piece of text answers the query. */
function matches(text: string, query: string): boolean {
  return normalizeQuery(text).includes(query);
}

/** Whether a guest answers the query, by name or by vmid. */
function guestMatches(guest: Guest, query: string): boolean {
  return matches(guest.name, query) || String(guest.vmid).includes(query);
}

/** Whether a node answers it, by its own name or through one of its guests. */
function nodeMatches(node: Node, query: string): boolean {
  return (
    matches(node.name, query) ||
    (node.guests ?? []).some((guest) => guestMatches(guest, query))
  );
}

/**
 * The clusters as the tree should show them for this query.
 *
 * An empty query returns the input untouched — the same array, not a copy, so
 * that nothing re-renders on a keystroke that changes nothing.
 */
export function filterClusters(
  clusters: ClusterOverview[],
  rawQuery: string,
): ClusterOverview[] {
  const query = normalizeQuery(rawQuery);
  if (query === "") {
    return clusters;
  }

  const out: ClusterOverview[] = [];
  for (const cluster of clusters) {
    const nodes = cluster.nodes;
    // The cluster itself answers: everything under it is a result.
    if (matches(cluster.name, query) || matches(cluster.id, query)) {
      out.push(cluster);
      continue;
    }

    const keptNodes: Node[] = [];
    for (const node of nodes) {
      if (!nodeMatches(node, query)) {
        continue;
      }
      // The node answers: it keeps all its guests, since they are what it
      // holds. Otherwise only the guests that answer stay.
      keptNodes.push(
        matches(node.name, query)
          ? node
          : { ...node, guests: (node.guests ?? []).filter((g) => guestMatches(g, query)) },
      );
    }
    if (keptNodes.length > 0) {
      out.push({ ...cluster, nodes: keptNodes });
    }
  }
  return out;
}

/**
 * How many rows the query matched, counting the rows themselves and not their
 * ancestors: a cluster shown only because one of its guests answered is not a
 * result, it is the way to one.
 */
export function countMatches(clusters: ClusterOverview[], rawQuery: string): number {
  const query = normalizeQuery(rawQuery);
  if (query === "") {
    return 0;
  }

  let found = 0;
  for (const cluster of clusters) {
    if (matches(cluster.name, query) || matches(cluster.id, query)) {
      found += 1;
    }
    for (const node of cluster.nodes) {
      if (matches(node.name, query)) {
        found += 1;
      }
      for (const guest of node.guests ?? []) {
        if (guestMatches(guest, query)) {
          found += 1;
        }
      }
    }
  }
  return found;
}

/**
 * Where Enter in the search field goes: the first result in tree order, which
 * is the first row the operator sees.
 */
export function firstMatch(clusters: ClusterOverview[], rawQuery: string): Route | null {
  const query = normalizeQuery(rawQuery);
  if (query === "") {
    return null;
  }

  for (const cluster of clusters) {
    if (matches(cluster.name, query) || matches(cluster.id, query)) {
      return { kind: "cluster", clusterId: cluster.id };
    }
    for (const node of cluster.nodes) {
      if (matches(node.name, query)) {
        return { kind: "node", clusterId: cluster.id, node: node.name };
      }
      for (const guest of node.guests ?? []) {
        if (guestMatches(guest, query)) {
          return { kind: "guest", clusterId: cluster.id, vmid: guest.vmid };
        }
      }
    }
  }
  return null;
}

/** `1 résultat` / `4 résultats` / `Aucun résultat`, for the live region. */
export function formatMatchCount(count: number): string {
  if (count === 0) return "Aucun résultat";
  return count === 1 ? "1 résultat" : `${String(count)} résultats`;
}
