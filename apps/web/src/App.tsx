/**
 * Application root: polls the overview and hands it to the shell.
 *
 * The selection is held here because the top bar and the tree share it. The
 * tree speaks the richer TreeSelection (a cluster, a node, a guest) while the
 * top bar only knows about clusters, so this is where the two are reconciled.
 */
import { useCallback, useMemo, useState } from "react";

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
import { useTheme } from "@/lib/useTheme";
import { ClustersOverview } from "@/screens/ClustersOverview";

/** null means "every cluster", which is the multi-cluster default view. */
export type SelectedClusterId = string | null;

export function App() {
  const { data, error, isLoading, isStale, lastUpdatedAt, refresh } = useOverview();
  const [selection, setSelection] = useState<TreeSelection>({ kind: "all" });
  const [search, setSearch] = useState("");
  // The theme belongs to the whole document, so it is held here and the top bar
  // stays a controlled component.
  const { preference: themePreference, setPreference: setThemePreference } =
    useTheme();

  const selectedClusterId: SelectedClusterId =
    selection.kind === "all" ? null : selection.clusterId;

  const selectCluster = useCallback((id: SelectedClusterId) => {
    setSelection(id === null ? { kind: "all" } : { kind: "cluster", clusterId: id });
  }, []);

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

  return (
    <AppShell
      topBar={
        <TopBar
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
          onSelect={setSelection}
        />
      }
    >
      {isLoading ? (
        <LoadingView />
      ) : visible === null ? (
        <ErrorView error={error ?? new Error("overview unavailable")} onRetry={refresh} />
      ) : (
        <>
          {/* Never hides the data underneath: it only says they are old. */}
          {isStale ? (
            <StaleBanner lastUpdatedAt={lastUpdatedAt} onRetry={refresh} />
          ) : null}
          {visible.clusters.length === 0 ? (
            <EmptyView
              title="Aucun cluster à afficher"
              hint="Ajoutez un cluster dans la configuration de moxyd."
            />
          ) : (
            <ClustersOverview
              overview={visible}
              onSelectCluster={(id) => {
                setSelection({ kind: "cluster", clusterId: id });
              }}
            />
          )}
        </>
      )}
    </AppShell>
  );
}
