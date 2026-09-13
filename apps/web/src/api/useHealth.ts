/**
 * One-shot read of the daemon's liveness answer, for the version in the bar.
 *
 * Unlike every other hook here it does not poll: the version of a running
 * moxyd cannot change under it, and a new one ships a new bundle, which means a
 * new page. A single request at mount is the whole cost.
 *
 * A failure is silent and leaves the version unknown. The bar then shows the
 * wordmark alone, and the outage is reported where it belongs — the overview,
 * which goes stale and says so.
 */
import { useEffect, useState } from "react";

import { fetchHealth } from "@/api/client";
import type { Health } from "@/api/client";

export function useHealth(): Health | null {
  const [health, setHealth] = useState<Health | null>(null);

  useEffect(() => {
    const controller = new AbortController();

    fetchHealth(controller.signal).then(
      (value) => {
        // The abort makes the answer of an unmounted component a no-op,
        // including StrictMode's rehearsal mount.
        if (!controller.signal.aborted) {
          setHealth(value);
        }
      },
      () => {
        // Nothing to report: see the note above.
      },
    );

    return () => {
      controller.abort();
    };
  }, []);

  return health;
}
