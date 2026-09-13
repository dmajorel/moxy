/**
 * Application root: polls the overview and hands it to the shell.
 *
 * THE URL IS THE SELECTION. The screen on display is derived from the address
 * bar, not from a piece of React state, so a refresh keeps the node open, a
 * pasted link opens it for someone else, and the back button goes back. The
 * tree and the top bar navigate; nothing here remembers where it was.
 *
 * The tree speaks the richer TreeSelection — a cluster, a node, a guest AND
 * the node hosting it — while a URL names no node for a guest: a guest
 * migrates, and a link pinning the node it was on would rot the moment it
 * moved. The hosting node is looked up in the overview, which is where the
 * truth about it lives, and that reconciliation happens here.
 */
import { useCallback, useMemo, useState } from "react";

import type { Overview } from "@/api/types";
import { useClusterSeries } from "@/api/useDetail";
import { useHealth } from "@/api/useHealth";
import { useOverview } from "@/api/useOverview";
import { AppShell } from "@/components/AppShell";
import { ClusterTree, type TreeSelection } from "@/components/ClusterTree";
import {
  EmptyView,
  ErrorView,
  LoadingView,
  StaleBanner,
} from "@/components/StateViews";
import { TopBar } from "@/components/TopBar";
import { filterOverview } from "@/lib/overview";
import type { Route } from "@/lib/routes";
import { useDocumentTitle } from "@/lib/useDocumentTitle";
import { useRoute, useNavigate } from "@/lib/useLocation";
import { useTheme } from "@/lib/useTheme";
import { ClusterJournal } from "@/screens/ClusterJournal";
import { ClustersOverview } from "@/screens/ClustersOverview";
import { GuestRoute, NodeRoute } from "@/screens/DetailRoutes";

/** null means "every cluster", which is the multi-cluster default view. */
export type SelectedClusterId = string | null;

export function App() {
  const { data, error, isLoading, isStale, lastUpdatedAt, refresh } = useOverview();
  // Read once at mount and never again: it is the version of the daemon this
  // bundle was served by, which an operator quotes in a ticket.
  const health = useHealth();
  const route = useRoute();
  const navigate = useNavigate();
  const [search, setSearch] = useState("");
  // The theme belongs to the whole document, so it is held here and the top bar
  // stays a controlled component.
  const { preference: themePreference, setPreference: setThemePreference } =
    useTheme();

  const selectedClusterId: SelectedClusterId =
    route.kind === "all" || route.kind === "notFound" ? null : route.clusterId;

  const selectCluster = useCallback(
    (id: SelectedClusterId) => {
      // REPLACE, not push: narrowing the filter refines the view one is
      // already on, it does not go anywhere. Pushing it would make the back
      // button undo a menu choice one click at a time.
      navigate.replace(id === null ? { kind: "all" } : { kind: "cluster", clusterId: id });
    },
    [navigate],
  );

  // The way out of a detail screen whose object no longer exists, and of a URL
  // that designates nothing.
  const backToOverview = useCallback(() => {
    navigate.push({ kind: "all" });
  }, [navigate]);

  const selectFromTree = useCallback(
    (selection: TreeSelection) => {
      navigate.push(routeOf(selection));
    },
    [navigate],
  );

  const clusters = useMemo(
    () =>
      (data?.clusters ?? []).map((cluster) => ({
        id: cluster.id,
        name: cluster.name,
        status: cluster.status,
      })),
    [data],
  );

  const visible = useMemo(
    () => (data === null ? null : filterOverview(data, selectedClusterId)),
    [data, selectedClusterId],
  );

  // The tree's own shape of the selection, with the hosting node of a guest
  // resolved from the overview. Until the overview has arrived the node is
  // unknown, which only means the tree has not expanded that branch yet — the
  // detail screen does not need it.
  const selection = useMemo(() => selectionOf(route, data), [route, data]);

  const clusterName =
    selectedClusterId === null || data === null
      ? null
      : clusterNameOf(data, selectedClusterId);
  useDocumentTitle(...titleParts(route, clusterName));

  // The hour every card draws. It is polled apart from the overview and on its
  // own, slower cadence: RRD only moves once a minute, and the cards must not
  // wait on it — a chart that has not arrived costs a curve, not a card.
  const usage = useClusterSeries(clusters.map((cluster) => cluster.id));

  return (
    <AppShell
      topBar={
        <TopBar
          version={health?.version}
          clusters={clusters}
          selectedClusterId={selectedClusterId}
          onSelectCluster={selectCluster}
          value={search}
          onValueChange={setSearch}
          alertCount={data?.totals.alerts ?? 0}
          themePreference={themePreference}
          onThemePreferenceChange={setThemePreference}
          // No authentication yet: the avatar is a placeholder, not a signed-in
          // user. See the loopback warning in the README.
          userInitials="?"
          userName="Authentification non configurée"
        />
      }
      sidebar={
        <ClusterTree
          clusters={data?.clusters ?? []}
          selection={selection}
          onSelect={selectFromTree}
        />
      }
    >
      {/*
        The unknown path is answered before anything else, and without waiting
        for the overview: nothing about it depends on data, and making an
        operator watch a spinner before being told the address is wrong adds a
        delay to a message that is already bad news.
      */}
      {route.kind === "notFound" ? (
        <EmptyView
          title="Objet introuvable"
          hint="Cette adresse ne désigne ni un cluster, ni un nœud, ni une machine. Revenez à la vue d’ensemble pour retrouver ce que moxy connaît."
        />
      ) : isLoading ? (
        <LoadingView />
      ) : visible === null ? (
        <ErrorView error={error ?? new Error("overview unavailable")} onRetry={refresh} />
      ) : (
        <>
          {/* Never hides the data underneath: it only says they are old. */}
          {isStale ? (
            <StaleBanner lastUpdatedAt={lastUpdatedAt} onRetry={refresh} />
          ) : null}
          {route.kind === "node" ? (
            <NodeRoute
              cluster={route.clusterId}
              clusterName={clusterNameOf(visible, route.clusterId)}
              node={route.node}
              threshold={visible.thresholds.memory}
              onBackToOverview={backToOverview}
              onSelectGuest={(vmid) => {
                // The same route the tree emits, so the sidebar follows the
                // move: it opens the ancestors of whatever arrives.
                navigate.push({ kind: "guest", clusterId: route.clusterId, vmid });
              }}
            />
          ) : route.kind === "guest" ? (
            <GuestRoute
              cluster={route.clusterId}
              clusterName={clusterNameOf(visible, route.clusterId)}
              vmid={route.vmid}
              threshold={visible.thresholds.memory}
              onBackToOverview={backToOverview}
            />
          ) : visible.clusters.length === 0 ? (
            <EmptyView
              title="Aucun cluster à afficher"
              hint="Ajoutez un cluster dans la configuration de moxyd."
            />
          ) : (
            <>
              <ClustersOverview
                overview={visible}
                usage={usage.data ?? undefined}
                onSelectCluster={(id) => {
                  navigate.push({ kind: "cluster", clusterId: id });
                }}
              />
              {/* The journal belongs to one cluster; across all of them it
                  would mix unrelated histories into an unreadable stream. */}
              {route.kind === "cluster" && (
                <ClusterJournal cluster={route.clusterId} className="mt-3.5" />
              )}
            </>
          )}
        </>
      )}
    </AppShell>
  );
}

/** The route a tree row stands for. A guest drops the node it is hosted by. */
function routeOf(selection: TreeSelection): Route {
  switch (selection.kind) {
    case "cluster":
      return { kind: "cluster", clusterId: selection.clusterId };
    case "node":
      return { kind: "node", clusterId: selection.clusterId, node: selection.node };
    case "guest":
      return { kind: "guest", clusterId: selection.clusterId, vmid: selection.vmid };
    case "all":
      return { kind: "all" };
    default:
      return { kind: "all" };
  }
}

/**
 * The tree's selection for the current route.
 *
 * Only the guest case needs the overview, to name the node hosting it. A guest
 * nobody has heard of — an overview that has not arrived, a vmid that is not
 * in it — still selects: the detail screen fetches by cluster and vmid, and
 * the tree merely fails to expand a branch it cannot find.
 */
function selectionOf(route: Route, overview: Overview | null): TreeSelection {
  switch (route.kind) {
    case "cluster":
      return { kind: "cluster", clusterId: route.clusterId };
    case "node":
      return { kind: "node", clusterId: route.clusterId, node: route.node };
    case "guest":
      return {
        kind: "guest",
        clusterId: route.clusterId,
        node: hostOf(overview, route.clusterId, route.vmid) ?? "",
        vmid: route.vmid,
      };
    case "all":
    case "notFound":
      return { kind: "all" };
    default:
      return { kind: "all" };
  }
}

/** The node hosting a guest, as the last overview saw it. */
function hostOf(overview: Overview | null, clusterId: string, vmid: number): string | null {
  const cluster = overview?.clusters.find((entry) => entry.id === clusterId);
  for (const node of cluster?.nodes ?? []) {
    if (node.guests.some((guest) => guest.vmid === vmid)) {
      return node.name;
    }
  }
  return null;
}

/**
 * The tab title, most specific first.
 *
 * The object is named before its cluster because that is what distinguishes
 * one tab from the next, and a truncated tab shows its beginning.
 */
function titleParts(route: Route, clusterName: string | null): (string | null)[] {
  switch (route.kind) {
    case "cluster":
      return [clusterName ?? route.clusterId];
    case "node":
      return [route.node, clusterName ?? route.clusterId];
    case "guest":
      return [`VM ${String(route.vmid)}`, clusterName ?? route.clusterId];
    case "notFound":
      return ["Objet introuvable"];
    case "all":
      return ["Clusters"];
    default:
      return ["Clusters"];
  }
}

/**
 * Display name of a cluster, falling back to its id.
 *
 * The detail screens are reached from the tree, so the cluster is always in
 * the overview — but a cluster removed from the configuration between two
 * polls, or a link to one that never existed, must not blank the heading.
 */
function clusterNameOf(overview: Overview, id: string): string {
  return overview.clusters.find((cluster) => cluster.id === id)?.name ?? id;
}
