/**
 * Polling machinery shared by every screen.
 *
 * It mirrors the backend's own philosophy: a stale reading beats a blank
 * screen. A failed poll never clears the last good value, it only flags it as
 * stale so the UI can say so.
 */
import { useCallback, useEffect, useRef, useState } from "react";

import { isAbortError } from "@/api/client";

/** The backend refreshes its cache on this cadence; polling faster buys nothing. */
export const POLL_INTERVAL_MS = 5_000;

/**
 * Series cover an hour or more, so a slow cadence is plenty and each request
 * costs an RRD read upstream.
 */
export const SERIES_POLL_INTERVAL_MS = 60_000;

/**
 * The ceiling the backoff climbs to.
 *
 * A cluster that has been down for ten minutes is not news that arrives any
 * sooner for being asked every five seconds; a minute is short enough that a
 * recovery is noticed while the operator is still looking at the screen.
 */
export const MAX_POLL_INTERVAL_MS = 60_000;

export interface ResourceState<T> {
  /** Last successful reading, kept across failures. */
  data: T | null;
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

interface Snapshot<T> {
  data: T | null;
  error: Error | null;
  lastUpdatedAt: Date | null;
}

function emptySnapshot<T>(): Snapshot<T> {
  return { data: null, error: null, lastUpdatedAt: null };
}

/**
 * Polls `fetcher` until unmount.
 *
 * `fetcher` doubles as the identity of what is being watched: when it changes —
 * another node selected, another timeframe — the previous reading is dropped
 * rather than shown under the new heading. Callers must therefore wrap it in
 * useCallback keyed on their parameters.
 */
export function usePolledResource<T>(
  fetcher: (signal: AbortSignal) => Promise<T>,
  intervalMs: number = POLL_INTERVAL_MS,
): ResourceState<T> {
  const [snapshot, setSnapshot] = useState<Snapshot<T>>(emptySnapshot<T>);
  /** Non-null exactly while a request is in flight; also the identity token
   * used to discard the answers of superseded or unmounted requests. */
  const inFlight = useRef<AbortController | null>(null);
  /**
   * A chain of timeouts rather than one interval: the delay has to change
   * between ticks, and an interval cannot.
   */
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);
  /** Consecutive failures, which is what the backoff is a function of. */
  const failures = useRef(0);

  const clear = useCallback(() => {
    if (timer.current !== null) {
      clearTimeout(timer.current);
      timer.current = null;
    }
  }, []);

  /**
   * Arms the next tick, unless the tab is hidden.
   *
   * NOTHING IS POLLED WHILE THE TAB IS HIDDEN. A background tab kept asking —
   * throttled by the browser to about once a minute after a few minutes, which
   * is worse than useless: on returning, the operator looked at a reading that
   * could be a minute old, with no immediate tick, so a node that went offline
   * fifty seconds ago was still green.
   */
  const schedule = useCallback(() => {
    clear();
    if (typeof document !== "undefined" && document.visibilityState === "hidden") {
      return;
    }
    const delay = Math.min(
      intervalMs * 2 ** Math.max(0, failures.current - 1),
      MAX_POLL_INTERVAL_MS,
    );
    timer.current = setTimeout(() => {
      loadRef.current(false);
    }, delay);
  }, [clear, intervalMs]);

  const load = useCallback(
    (force: boolean) => {
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

      fetcher(controller.signal).then(
        (value) => {
          if (inFlight.current !== controller) {
            return;
          }
          inFlight.current = null;
          failures.current = 0;
          setSnapshot({ data: value, error: null, lastUpdatedAt: new Date() });
          schedule();
        },
        (cause: unknown) => {
          if (inFlight.current !== controller) {
            return;
          }
          inFlight.current = null;
          if (isAbortError(cause)) {
            // Superseded or unmounted: whoever aborted decides what comes next.
            return;
          }
          // Ten failures used to mean ten requests a minute against something
          // that is not answering; offline, every tick produced the same
          // ApiRequestError(0).
          failures.current += 1;
          setSnapshot((previous) => ({
            data: previous.data,
            error: asError(cause),
            lastUpdatedAt: previous.lastUpdatedAt,
          }));
          schedule();
        },
      );
    },
    [fetcher, schedule],
  );

  /**
   * The current load, reachable from a timeout without making `schedule`
   * depend on it: the two call each other, and a cycle in the dependency lists
   * would re-arm the chain on every render.
   */
  const loadRef = useRef(load);
  loadRef.current = load;

  const refresh = useCallback(() => {
    // An explicit retry is a fresh start: the operator asked, so the next
    // answer is not made to wait for a backoff earned by earlier failures.
    failures.current = 0;
    load(true);
  }, [load]);

  useEffect(() => {
    // A new fetcher means a new subject: showing the previous one's figures
    // under the new heading would be worse than showing nothing.
    setSnapshot(emptySnapshot<T>());
    failures.current = 0;
    load(true);
    return () => {
      clear();
      // Dropping the token first makes the pending answer a no-op, so an
      // unmount — including StrictMode's rehearsal one — writes no state.
      const controller = inFlight.current;
      inFlight.current = null;
      controller?.abort();
    };
  }, [load, clear]);

  /**
   * Coming back to the tab, and coming back online, both mean the same thing:
   * what is on screen may be a minute old, and the answer is one request away.
   */
  useEffect(() => {
    function wake() {
      if (document.visibilityState === "hidden") {
        clear();
        return;
      }
      failures.current = 0;
      loadRef.current(true);
    }
    document.addEventListener("visibilitychange", wake);
    window.addEventListener("online", wake);
    return () => {
      document.removeEventListener("visibilitychange", wake);
      window.removeEventListener("online", wake);
    };
  }, [clear]);

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
