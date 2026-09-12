/**
 * Hooks behind the per-object screens.
 *
 * Unlike the overview, which the backend polls for every cluster, these views
 * are fetched only while their object is on screen. The backend absorbs the
 * repetition with a short cache and a single-flight lock, so a refresh here
 * costs one upstream call at most, however many tabs are open.
 */
import { useCallback } from "react";

import {
  fetchGuest,
  fetchGuestSeries,
  fetchMaintenancePlan,
  fetchNode,
  fetchNodeSeries,
  fetchTasks,
} from "@/api/client";
import type {
  GuestDetail,
  MaintenancePlan,
  NodeDetail,
  Series,
  Tasks,
  Timeframe,
} from "@/api/types";
import type { ResourceState } from "@/api/usePolledResource";
import {
  SERIES_POLL_INTERVAL_MS,
  usePolledResource,
} from "@/api/usePolledResource";

/** How many recent tasks the cluster journal asks for. */
export const TASK_LIMIT = 25;

export function useNode(cluster: string, node: string): ResourceState<NodeDetail> {
  const fetcher = useCallback(
    (signal: AbortSignal) => fetchNode(cluster, node, signal),
    [cluster, node],
  );
  return usePolledResource(fetcher);
}

export function useGuest(cluster: string, vmid: number): ResourceState<GuestDetail> {
  const fetcher = useCallback(
    (signal: AbortSignal) => fetchGuest(cluster, vmid, signal),
    [cluster, vmid],
  );
  return usePolledResource(fetcher);
}

export function useNodeSeries(
  cluster: string,
  node: string,
  timeframe: Timeframe,
): ResourceState<Series> {
  const fetcher = useCallback(
    (signal: AbortSignal) => fetchNodeSeries(cluster, node, timeframe, signal),
    [cluster, node, timeframe],
  );
  return usePolledResource(fetcher, SERIES_POLL_INTERVAL_MS);
}

export function useGuestSeries(
  cluster: string,
  vmid: number,
  timeframe: Timeframe,
): ResourceState<Series> {
  const fetcher = useCallback(
    (signal: AbortSignal) => fetchGuestSeries(cluster, vmid, timeframe, signal),
    [cluster, vmid, timeframe],
  );
  return usePolledResource(fetcher, SERIES_POLL_INTERVAL_MS);
}

export function useTasks(cluster: string, limit: number = TASK_LIMIT): ResourceState<Tasks> {
  const fetcher = useCallback(
    (signal: AbortSignal) => fetchTasks(cluster, limit, signal),
    [cluster, limit],
  );
  return usePolledResource(fetcher);
}

/**
 * Drain plan for a node.
 *
 * Polled like everything else while the dialog is open: the plan is only
 * meaningful against the cluster as it is now, and a stale one would send an
 * operator to a node that has since filled up.
 */
export function useMaintenancePlan(
  cluster: string,
  node: string,
): ResourceState<MaintenancePlan> {
  const fetcher = useCallback(
    (signal: AbortSignal) => fetchMaintenancePlan(cluster, node, signal),
    [cluster, node],
  );
  return usePolledResource(fetcher);
}
