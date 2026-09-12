/**
 * Polling hook behind every screen.
 *
 * It mirrors the backend's own philosophy: a stale reading beats a blank
 * screen. A failed poll never clears the last good overview, it only flags it
 * as stale so the UI can say so.
 */
import { useCallback, useEffect, useRef, useState } from "react";

import { fetchOverview, isAbortError } from "@/api/client";
import type { Overview } from "@/api/types";

/** The backend refreshes its cache on this cadence; polling faster buys nothing. */
export const POLL_INTERVAL_MS = 5_000;

export interface OverviewState {
  /** Last successful reading, kept across failures. */
  data: Overview | null;
  /** Failure of the most recent attempt, cleared by the next success. */
  error: Error | null;
  /** No reading yet and nothing has failed yet: the very first attempt. */
  isLoading: boolean;
  /** Data is on screen but the latest attempt failed. */
  isStale: boolean;
  lastUpdatedAt: Date | null;
  /** Forces an immediate request and restarts the polling cycle. */
  refresh: () => void;
}

interface Snapshot {
  data: Overview | null;
  error: Error | null;
  lastUpdatedAt: Date | null;
}

const initialSnapshot: Snapshot = {
  data: null,
  error: null,
  lastUpdatedAt: null,
};

export function useOverview(): OverviewState {
  const [snapshot, setSnapshot] = useState<Snapshot>(initialSnapshot);
  /** Non-null exactly while a request is in flight; also the identity token
   * used to discard the answers of superseded or unmounted requests. */
  const inFlight = useRef<AbortController | null>(null);
  const timer = useRef<ReturnType<typeof setInterval> | null>(null);

  const load = useCallback((force: boolean) => {
    if (inFlight.current !== null) {
      // A scheduled tick never stacks on a slow backend; an explicit refresh
      // supersedes the pending request instead of waiting for it.
      if (!force) {
        return;
      }
      inFlight.current.abort();
    }
    const controller = new AbortController();
    inFlight.current = controller;

    fetchOverview(controller.signal).then(
      (overview) => {
        if (inFlight.current !== controller) {
          return;
        }
        inFlight.current = null;
        setSnapshot({
          data: overview,
          error: null,
          lastUpdatedAt: new Date(),
        });
      },
      (cause: unknown) => {
        if (inFlight.current !== controller) {
          return;
        }
        inFlight.current = null;
        if (isAbortError(cause)) {
          return;
        }
        setSnapshot((previous) => ({
          data: previous.data,
          error: asError(cause),
          lastUpdatedAt: previous.lastUpdatedAt,
        }));
      },
    );
  }, []);

  const schedule = useCallback(() => {
    if (timer.current !== null) {
      clearInterval(timer.current);
    }
    timer.current = setInterval(() => {
      load(false);
    }, POLL_INTERVAL_MS);
  }, [load]);

  const refresh = useCallback(() => {
    load(true);
    schedule();
  }, [load, schedule]);

  useEffect(() => {
    load(true);
    schedule();
    return () => {
      if (timer.current !== null) {
        clearInterval(timer.current);
        timer.current = null;
      }
      // Dropping the token first makes the pending answer a no-op, so an
      // unmount — including StrictMode's rehearsal one — writes no state.
      const controller = inFlight.current;
      inFlight.current = null;
      controller?.abort();
    };
  }, [load, schedule]);

  return {
    data: snapshot.data,
    error: snapshot.error,
    isLoading: snapshot.data === null && snapshot.error === null,
    isStale: snapshot.data !== null && snapshot.error !== null,
    lastUpdatedAt: snapshot.lastUpdatedAt,
    refresh,
  };
}

function asError(cause: unknown): Error {
  return cause instanceof Error ? cause : new Error(String(cause));
}
