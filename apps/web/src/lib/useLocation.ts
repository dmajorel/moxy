import { useCallback, useSyncExternalStore } from "react";

import { formatPath, parsePath, type Route } from "./routes";

/**
 * The browser's history, read as React state.
 *
 * No router dependency: the whole of it is `pushState`, `popstate` and a
 * subscription, and a routing library would be more code to audit than the
 * forty lines below. `useSyncExternalStore` is what makes this correct rather
 * than merely working — it is the hook built for a mutable source outside
 * React, and it keeps a concurrent render from painting a screen for a URL
 * that has already changed.
 */

/** Subscribers, notified on a back/forward AND on our own navigations. */
const listeners = new Set<() => void>();

function subscribe(listener: () => void): () => void {
  listeners.add(listener);
  window.addEventListener("popstate", listener);
  return () => {
    listeners.delete(listener);
    window.removeEventListener("popstate", listener);
  };
}

/**
 * The current pathname.
 *
 * pushState does NOT fire popstate — that event is the back and forward
 * buttons alone — so every navigation below tells the subscribers itself.
 */
function currentPath(): string {
  return window.location.pathname;
}

/** The server snapshot: there is no server rendering, and none is planned. */
function serverPath(): string {
  return "/";
}

function notify(): void {
  for (const listener of listeners) {
    listener();
  }
}

/** The route the address bar currently names. */
export function useRoute(): Route {
  const pathname = useSyncExternalStore(subscribe, currentPath, serverPath);
  return parsePath(pathname);
}

export interface Navigate {
  /** Adds a history entry: the back button returns to where the user was. */
  push: (route: Route) => void;
  /**
   * Replaces the current entry. Used where a step back would be surprising —
   * narrowing the cluster filter is a refinement of the view, not a place, and
   * pushing it would make the back button undo a menu choice one click at a
   * time.
   */
  replace: (route: Route) => void;
}

/**
 * Navigation that keeps `useRoute` in step.
 *
 * A navigation to the path already displayed is dropped rather than pushed: a
 * second click on the row that is already selected must not grow the history
 * by an entry that changes nothing.
 */
export function useNavigate(): Navigate {
  const push = useCallback((route: Route) => {
    go(route, false);
  }, []);
  const replace = useCallback((route: Route) => {
    go(route, true);
  }, []);
  return { push, replace };
}

function go(route: Route, replace: boolean): void {
  const path = formatPath(route);
  if (path === window.location.pathname) {
    return;
  }
  // The query and the fragment are carried over: nothing writes one today, and
  // dropping what a future one puts there would be a silent loss.
  const target = path + window.location.search + window.location.hash;
  if (replace) {
    window.history.replaceState(null, "", target);
  } else {
    window.history.pushState(null, "", target);
  }
  notify();
}
