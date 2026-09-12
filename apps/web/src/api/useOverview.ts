/**
 * Polling hook behind the cluster overview.
 *
 * The mechanics live in usePolledResource, which every screen shares; this file
 * only binds it to the overview endpoint.
 */
import { useCallback } from "react";

import { fetchOverview } from "@/api/client";
import type { Overview } from "@/api/types";
import type { ResourceState } from "@/api/usePolledResource";
import { usePolledResource } from "@/api/usePolledResource";

export { POLL_INTERVAL_MS } from "@/api/usePolledResource";

export type OverviewState = ResourceState<Overview>;

export function useOverview(): OverviewState {
  const fetcher = useCallback(
    (signal: AbortSignal) => fetchOverview(signal),
    [],
  );
  return usePolledResource(fetcher);
}
