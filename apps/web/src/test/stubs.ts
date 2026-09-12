/**
 * Browser APIs jsdom does not provide, or does not provide broken.
 *
 * `window.matchMedia` is simply not a function in jsdom, so anything exercising
 * the system colour preference has to bring its own — and the production code
 * has to survive its absence, which is why these install and uninstall per test
 * instead of the setup file doing it once for everyone.
 */

export interface MatchMediaStub {
  /** Flips the preference and notifies every listener, as a desktop would. */
  setMatches: (matches: boolean) => void;
  /** Restores whatever `window.matchMedia` was before, including nothing. */
  restore: () => void;
  /** Listeners still attached; zero after a correct unsubscribe. */
  listenerCount: () => number;
}

/**
 * Installs a `prefers-color-scheme` stub answering `matches`.
 *
 * Only the modern addEventListener/removeEventListener pair is provided: the
 * legacy addListener fallback in lib/theme.ts is there for Safari 13, and a
 * stub that offers both would never exercise either branch on its own.
 */
export function stubMatchMedia(matches: boolean): MatchMediaStub {
  const listeners = new Set<(event: MediaQueryListEvent) => void>();
  let current = matches;

  const query = {
    get matches() {
      return current;
    },
    media: "(prefers-color-scheme: dark)",
    addEventListener(_type: string, listener: (event: MediaQueryListEvent) => void) {
      listeners.add(listener);
    },
    removeEventListener(_type: string, listener: (event: MediaQueryListEvent) => void) {
      listeners.delete(listener);
    },
  };

  const original = Object.getOwnPropertyDescriptor(window, "matchMedia");
  Object.defineProperty(window, "matchMedia", {
    value: () => query,
    configurable: true,
    writable: true,
  });

  return {
    setMatches(next: boolean) {
      current = next;
      for (const listener of listeners) {
        listener({ matches: next } as MediaQueryListEvent);
      }
    },
    restore() {
      if (original === undefined) {
        Reflect.deleteProperty(window, "matchMedia");
      } else {
        Object.defineProperty(window, "matchMedia", original);
      }
    },
    listenerCount() {
      return listeners.size;
    },
  };
}

/**
 * Makes every `window.localStorage` access throw, the way a Chrome window with
 * site data blocked does — the property itself throws there, before any method
 * is called.
 */
export function stubBrokenLocalStorage(): () => void {
  const original = Object.getOwnPropertyDescriptor(window, "localStorage");
  Object.defineProperty(window, "localStorage", {
    get() {
      throw new DOMException("access denied", "SecurityError");
    },
    configurable: true,
  });

  return () => {
    if (original === undefined) {
      Reflect.deleteProperty(window, "localStorage");
    } else {
      Object.defineProperty(window, "localStorage", original);
    }
  };
}
