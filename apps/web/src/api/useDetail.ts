/**
 * Hooks behind the per-object screens.
 *
 * Unlike the overview, which the backend polls for every cluster, these views
 * are fetched only while their object is on screen. The backend absorbs the
 * repetition with a short cache and a single-flight lock, so a refresh here
 * costs one upstream call at most, however many tabs are open.
 */
import { useCallback, useEffect, useRef, useState } from "react";

import {
  fetchClusterSeries,
  fetchGuest,
  fetchGuestSeries,
  fetchGuestTasks,
  fetchMaintenancePlan,
  fetchNode,
  fetchNodeSeries,
  fetchTasks,
  isAbortError,
  requestMaintenance,
} from "@/api/client";
import type {
  GuestDetail,
  MaintenanceAction,
  MaintenancePlan,
  MaintenanceResult,
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

/** How many recent tasks a log asks for, the cluster journal's and a guest's. */
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
 * The tasks of one guest, asked of the backend rather than sieved out of the
 * cluster journal: the journal only carries its most recent entries, and a
 * cluster that backs up ninety guests a night pushes any one machine's lines
 * out of them within the hour.
 */
export function useGuestTasks(
  cluster: string,
  vmid: number,
  limit: number = TASK_LIMIT,
): ResourceState<Tasks> {
  const fetcher = useCallback(
    (signal: AbortSignal) => fetchGuestTasks(cluster, vmid, limit, signal),
    [cluster, vmid, limit],
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

/** Where one maintenance request is in its life. */
export type MaintenancePhase = "idle" | "running" | "done" | "failed";

export interface MaintenanceCommand {
  phase: MaintenancePhase;
  /** The answer, kept after `done`. Null in every other phase. */
  result: MaintenanceResult | null;
  /** Why it failed, kept after `failed`. Null in every other phase. */
  error: Error | null;
  /** Sends one request. Ignored while another is in flight. */
  run: (action: MaintenanceAction) => void;
}

/**
 * The one call of this interface that writes, and therefore the one that is
 * NOT polled.
 *
 * It is a hook and not a bare function because three states have to be
 * rendered — in flight, accepted, refused — and the screen that renders them
 * must not be able to invent a fourth. `run` is a fire-and-forget: the answer
 * says the request went through, never that the node is drained, so there is
 * nothing here to keep refreshing. The real state arrives through the polling
 * the node view already does.
 *
 * A second click while a request is in flight is dropped rather than queued:
 * the backend answers `already_running` to a genuine second caller, and an
 * impatient operator pressing twice does not deserve that error.
 */
export function useMaintenanceCommand(
  cluster: string,
  node: string,
): MaintenanceCommand {
  const [phase, setPhase] = useState<MaintenancePhase>("idle");
  const [result, setResult] = useState<MaintenanceResult | null>(null);
  const [error, setError] = useState<Error | null>(null);
  // Guards the answer, not the request: a request that outlives its screen is
  // still worth finishing — the node is being drained either way — but writing
  // its answer into a component that is gone is not.
  const live = useRef(true);
  const inFlight = useRef(false);

  useEffect(() => {
    live.current = true;
    return () => {
      live.current = false;
    };
  }, []);

  // Another node selected under the same screen starts from nothing: an answer
  // about the node before it would read as an answer about this one.
  useEffect(() => {
    setPhase("idle");
    setResult(null);
    setError(null);
    inFlight.current = false;
  }, [cluster, node]);

  const run = useCallback(
    (action: MaintenanceAction) => {
      if (inFlight.current) {
        return;
      }
      inFlight.current = true;
      setPhase("running");
      setResult(null);
      setError(null);

      requestMaintenance(cluster, node, action).then(
        (answer) => {
          inFlight.current = false;
          if (!live.current) return;
          setResult(answer);
          setPhase("done");
        },
        (cause: unknown) => {
          inFlight.current = false;
          if (!live.current) return;
          setError(cause instanceof Error ? cause : new Error(String(cause)));
          setPhase("failed");
        },
      );
    },
    [cluster, node],
  );

  return { phase, result, error, run };
}
