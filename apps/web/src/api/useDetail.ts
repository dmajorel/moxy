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
  fetchClusterSeries,
  fetchGuest,
  fetchGuestSeries,
  fetchMaintenancePlan,
  fetchNode,
  fetchNodeSeries,
  fetchTasks,
  isAbortError,
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

/**
 * The hour drawn by every cluster card, fetched as one polled resource.
 *
 * The overview shows all the cards at once and a hook cannot be called in a
 * loop, so the requests go out side by side under a single resource. A cluster
 * that fails is simply missing from the map rather than failing the batch: one
 * unreachable cluster must not blank the charts of the others, which is the
 * rule useOverview already follows for the cards themselves.
 *
 * `clusters` is read through the joined key so that a caller may rebuild the
 * array on every render without restarting the polling cycle.
 */
export function useClusterSeries(
  clusters: string[],
  timeframe: Timeframe = "hour",
): ResourceState<Record<string, Series>> {
  const key = clusters.join("\u0000");

  const fetcher = useCallback(
    async (signal: AbortSignal): Promise<Record<string, Series>> => {
      const ids = key === "" ? [] : key.split("\u0000");
      const answers = await Promise.all(
        ids.map(async (id): Promise<[string, Series] | null> => {
          try {
            return [id, await fetchClusterSeries(id, timeframe, signal)];
          } catch (cause) {
            if (isAbortError(cause)) {
              throw cause;
            }
            return null;
          }
        }),
      );

      const series: Record<string, Series> = {};
      for (const answer of answers) {
        if (answer !== null) {
          series[answer[0]] = answer[1];
        }
      }
      return series;
    },
    [key, timeframe],
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
