/**
 * Containers that turn a tree selection into a fetched screen.
 *
 * They exist because hooks cannot be called conditionally: App decides *what*
 * is selected, and one of these decides *how* to load it.
 */
import { useState } from "react";

import type { Thresholds } from "@/api/types";
import {
  useGuest,
  useGuestSeries,
  useGuestTasks,
  useNode,
  useNodeSeries,
} from "@/api/useDetail";
import { ErrorBoundary } from "@/components/ErrorBoundary";
import { ErrorView, LoadingView, StaleBanner } from "@/components/StateViews";
import { GuestDetail } from "@/screens/GuestDetail";
import { MaintenancePlanDialog } from "@/screens/MaintenancePlanDialog";
import { NodeDetail } from "@/screens/NodeDetail";

interface CommonProps {
  cluster: string;
  clusterName: string;
  /**
   * Forwarded whole rather than picked apart here: the screens know which
   * resource each of their cards shows, this container does not.
   */
  thresholds: Thresholds;
  /**
   * Where to go when the object is gone.
   *
   * Only a 404 offers it — the error view decides — because that is the one
   * failure retrying cannot fix: what is deleted or renamed will not come back
   * by asking again, while the overview still lists what the cluster holds.
   */
  onBackToOverview: () => void;
}

export function NodeRoute({
  cluster,
  clusterName,
  thresholds,
  node,
  onBackToOverview,
  onSelectGuest,
}: CommonProps & { node: string; onSelectGuest: (vmid: number) => void }) {
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
        onBack={onBackToOverview}
      />
    );
  }

  return (
    <>
      {detail.isStale && (
        <StaleBanner
          lastUpdatedAt={detail.lastUpdatedAt}
          onRetry={detail.refresh}
        />
      )}
      {/*
        A boundary per screen, inside the one AppShell already puts around the
        pane. It is narrower on purpose: a node view that throws leaves the
        stale banner above it standing, so an operator still sees WHEN the
        data it choked on was read.
      */}
      <ErrorBoundary label="the node view">
        <NodeDetail
          node={detail.data}
          clusterName={clusterName}
          // A failing series must not take the whole screen down: the metrics
          // above it are still worth reading.
          series={series.data}
          thresholds={thresholds}
          onPlanMaintenance={() => {
            setPlanOpen(true);
          }}
          // The guest table is the natural way down from a node, so the screen
          // hands the selection back to App rather than holding one of its own.
          onSelectGuest={onSelectGuest}
        />
      </ErrorBoundary>
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
  thresholds,
  onBackToOverview,
  vmid,
}: CommonProps & { vmid: number }) {
  const detail = useGuest(cluster, vmid);
  const series = useGuestSeries(cluster, vmid, "hour");
  const tasks = useGuestTasks(cluster, vmid);

  if (detail.isLoading) {
    return <LoadingView />;
  }
  if (detail.data === null) {
    return (
      <ErrorView
        error={detail.error ?? new Error("guest unavailable")}
        onRetry={detail.refresh}
        onBack={onBackToOverview}
      />
    );
  }

  return (
    <>
      {detail.isStale && (
        <StaleBanner
          lastUpdatedAt={detail.lastUpdatedAt}
          onRetry={detail.refresh}
        />
      )}
      <ErrorBoundary label="the guest view">
        <GuestDetail
          guest={detail.data}
          clusterName={clusterName}
          series={series.data}
          // A failing log is no reason to blank the screen either: the metrics
          // above it still say what the machine is doing.
          tasks={tasks.data?.entries ?? []}
          thresholds={thresholds}
        />
      </ErrorBoundary>
    </>
  );
}
