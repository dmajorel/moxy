import { useEffect, useState } from "react";

/**
 * A clock that ticks, so a relative label does not freeze.
 *
 * "il y a 12 s" was computed at render time and recomputed only at the next
 * render — which, during an outage, is precisely what stops happening. An
 * operator watching a cluster go away saw the age of the reading stop at the
 * moment the polling failed, which is the one moment it matters.
 *
 * One clock for a whole screen rather than one per card: the tick re-renders
 * its owner, and eight cards each holding a timer would be eight renders a
 * second for one changing word.
 *
 * The clock stops while the tab is hidden, for the same reason the polling
 * does: nothing is being read, so nothing needs re-rendering.
 */
export function useNow(intervalMs = 1_000): Date {
  const [now, setNow] = useState(() => new Date());

  useEffect(() => {
    let timer: ReturnType<typeof setInterval> | null = null;

    function start() {
      if (timer !== null) return;
      timer = setInterval(() => {
        setNow(new Date());
      }, intervalMs);
    }

    function stop() {
      if (timer === null) return;
      clearInterval(timer);
      timer = null;
    }

    function onVisibility() {
      if (document.visibilityState === "hidden") {
        stop();
        return;
      }
      // Coming back: the label is as old as the time spent away, so it is
      // brought up to date before the next tick rather than after it.
      setNow(new Date());
      start();
    }

    if (document.visibilityState !== "hidden") {
      start();
    }
    document.addEventListener("visibilitychange", onVisibility);
    return () => {
      stop();
      document.removeEventListener("visibilitychange", onVisibility);
    };
  }, [intervalMs]);

  return now;
}
