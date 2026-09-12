/**
 * Containers that turn a tree selection into a fetched screen.
 *
 * They exist because hooks cannot be called conditionally: App decides *what*
 * is selected, and one of these decides *how* to load it.
 */
import { useState } from "react";

import { useGuest, useGuestSeries, useNode, useNodeSeries, useTasks } from "@/api/useDetail";
import { ErrorView, LoadingView, StaleBanner } from "@/components/StateViews";
import { GuestDetail } from "@/screens/GuestDetail";
import { MaintenancePlanDialog } from "@/screens/MaintenancePlanDialog";
import { NodeDetail } from "@/screens/NodeDetail";

interface CommonProps {
  cluster: string;
  clusterName: string;
  threshold: number;
}

export function NodeRoute({
  cluster,
  clusterName,
  threshold,
  node,
}: CommonProps & { node: string }) {
  const detail = useNode(cluster, node);
  const series = useNodeSeries(cluster, node, "hour");
  const [planOpen, setPlanOpen] = useState(false);

  if (detail.isLoading) {
    return <LoadingView />;
  }
  if (detail.data === null) {
    return (
      <ErrorView
        error={detail.error ?? new Error("node unavailable")}
        onRetry={detail.refresh}
      />
    );
  }

  return (
    <>
      {detail.isStale && (
        <StaleBanner lastUpdatedAt={detail.lastUpdatedAt} onRetry={detail.refresh} />
      )}
      <NodeDetail
        node={detail.data}
        clusterName={clusterName}
        // A failing series must not take the whole screen down: the metrics
        // above it are still worth reading.
        series={series.data}
        threshold={threshold}
        onPlanMaintenance={() => {
          setPlanOpen(true);
        }}
      />
      {planOpen && (
        <MaintenancePlanDialog
          cluster={cluster}
          clusterName={clusterName}
          node={node}
          onClose={() => {
            setPlanOpen(false);
          }}
        />
      )}
    </>
  );
}

export function GuestRoute({
  cluster,
  clusterName,
  threshold,
  vmid,
}: CommonProps & { vmid: number }) {
  const detail = useGuest(cluster, vmid);
  const series = useGuestSeries(cluster, vmid, "hour");
  const tasks = useTasks(cluster);

  if (detail.isLoading) {
    return <LoadingView />;
  }
  if (detail.data === null) {
    return (
      <ErrorView
        error={detail.error ?? new Error("guest unavailable")}
        onRetry={detail.refresh}
      />
    );
  }

  // PVE files a guest operation under its VMID, which is how the cluster
  // journal is narrowed to this machine.
  const ownTasks = (tasks.data?.entries ?? []).filter(
    (task) => task.id === String(vmid),
  );

  return (
    <>
      {detail.isStale && (
        <StaleBanner lastUpdatedAt={detail.lastUpdatedAt} onRetry={detail.refresh} />
      )}
      <GuestDetail
        guest={detail.data}
        clusterName={clusterName}
        series={series.data}
        tasks={ownTasks}
        threshold={threshold}
      />
    </>
  );
}
