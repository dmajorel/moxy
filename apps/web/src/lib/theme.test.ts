// Vite hands the file over as a string; no filesystem path to resolve, and the
// assertions below break the day the file moves rather than passing vacuously.
import indexHtml from "../../index.html?raw";


import { stubBrokenLocalStorage, stubMatchMedia } from "@/test/stubs";

import {
  applyTheme,
  isThemePreference,
  readStoredPreference,
  resolveTheme,
  storePreference,
  systemTheme,
  watchSystemTheme,
  THEME_ATTRIBUTE,
  THEME_STORAGE_KEY,
} from "./theme";

afterEach(() => {
  window.localStorage.clear();
  document.documentElement.removeAttribute(THEME_ATTRIBUTE);
});

describe("isThemePreference", () => {
  it("accepts the three preferences", () => {
    expect(isThemePreference("light")).toBe(true);
    expect(isThemePreference("dark")).toBe(true);
    expect(isThemePreference("system")).toBe(true);
  });

  it("rejects anything else", () => {
    expect(isThemePreference("sombre")).toBe(false);
    expect(isThemePreference(null)).toBe(false);
    expect(isThemePreference(undefined)).toBe(false);
    expect(isThemePreference(1)).toBe(false);
  });
});

describe("readStoredPreference", () => {
  it("reads back what was stored", () => {
    window.localStorage.setItem(THEME_STORAGE_KEY, "dark");

    expect(readStoredPreference()).toBe("dark");
  });

  it("falls back to system when nothing is stored", () => {
    expect(readStoredPreference()).toBe("system");
  });

  it("falls back to system on a corrupted value", () => {
    window.localStorage.setItem(THEME_STORAGE_KEY, "midnight");

    expect(readStoredPreference()).toBe("system");
  });

  it("falls back to system when storage is unavailable", () => {
    const restore = stubBrokenLocalStorage();
    try {
      expect(readStoredPreference()).toBe("system");
    } finally {
      restore();
    }
  });
});

describe("storePreference", () => {
  it("persists an explicit choice", () => {
    expect(storePreference("dark")).toBe(true);

    expect(window.localStorage.getItem(THEME_STORAGE_KEY)).toBe("dark");
  });

  it("clears the entry for system, since the absence is the default", () => {
    window.localStorage.setItem(THEME_STORAGE_KEY, "dark");

    expect(storePreference("system")).toBe(true);

    expect(window.localStorage.getItem(THEME_STORAGE_KEY)).toBeNull();
  });

  it("reports failure instead of throwing when storage is unavailable", () => {
    const restore = stubBrokenLocalStorage();
    try {
      expect(storePreference("dark")).toBe(false);
    } finally {
      restore();
    }
  });

  it("reports failure instead of throwing when the quota is spent", () => {
    const spy = vi
      .spyOn(Storage.prototype, "setItem")
      .mockImplementation(() => {
        throw new DOMException("quota exceeded", "QuotaExceededError");
      });
    try {
      expect(storePreference("light")).toBe(false);
    } finally {
      spy.mockRestore();
    }
  });
});

describe("systemTheme", () => {
  it("reports dark when the system asks for dark", () => {
    const media = stubMatchMedia(true);
    try {
      expect(systemTheme()).toBe("dark");
    } finally {
      media.restore();
    }
  });

  it("reports light when the system asks for light", () => {
    const media = stubMatchMedia(false);
    try {
      expect(systemTheme()).toBe("light");
    } finally {
      media.restore();
    }
  });

  it("reports light when the question cannot be asked", () => {
    expect(typeof window.matchMedia).toBe("undefined");

    expect(systemTheme()).toBe("light");
  });
});

describe("resolveTheme", () => {
  it("returns an explicit choice untouched, whatever the system says", () => {
    const media = stubMatchMedia(true);
    try {
      expect(resolveTheme("light")).toBe("light");
      expect(resolveTheme("dark")).toBe("dark");
    } finally {
      media.restore();
    }
  });

  it("defers to the system for the system preference", () => {
    const media = stubMatchMedia(true);
    try {
      expect(resolveTheme("system")).toBe("dark");
    } finally {
      media.restore();
    }
  });
});

describe("applyTheme", () => {
  it("stamps an explicit choice on the document element", () => {
    applyTheme("dark");

    expect(document.documentElement.getAttribute(THEME_ATTRIBUTE)).toBe("dark");
  });

  it("stamps light too, so it can override a dark system", () => {
    applyTheme("light");

    expect(document.documentElement.getAttribute(THEME_ATTRIBUTE)).toBe("light");
  });

  it("removes the attribute for system, leaving the media query in charge", () => {
    applyTheme("dark");

    applyTheme("system");

    expect(document.documentElement.hasAttribute(THEME_ATTRIBUTE)).toBe(false);
  });
});

describe("watchSystemTheme", () => {
  it("reports each flip of the system preference", () => {
    const media = stubMatchMedia(false);
    const onChange = vi.fn();
    try {
      const unsubscribe = watchSystemTheme(onChange);

      media.setMatches(true);
      media.setMatches(false);

      expect(onChange.mock.calls).toEqual([["dark"], ["light"]]);
      unsubscribe();
    } finally {
      media.restore();
    }
  });

  it("detaches its listener on unsubscribe", () => {
    const media = stubMatchMedia(false);
    try {
      const unsubscribe = watchSystemTheme(vi.fn());
      expect(media.listenerCount()).toBe(1);

      unsubscribe();

      expect(media.listenerCount()).toBe(0);
    } finally {
      media.restore();
    }
  });

  it("returns a usable unsubscribe when matchMedia is missing", () => {
    expect(typeof window.matchMedia).toBe("undefined");

    expect(() => {
      watchSystemTheme(vi.fn())();
    }).not.toThrow();
  });
});

/**
 * The inline script of index.html cannot import this module — it runs before
 * the bundle exists — so it repeats the key and the attribute by hand. Renaming
 * either one without the other would silently bring the flash of light theme
 * back, which no rendering test would catch.
 */
describe("the anti-flash script in index.html", () => {
  it("reads the same storage key as this module", () => {
    expect(indexHtml).toContain(`getItem("${THEME_STORAGE_KEY}")`);
  });

  it("stamps the same attribute as this module", () => {
    expect(indexHtml).toContain(`setAttribute("${THEME_ATTRIBUTE}", stored)`);
  });

  it("runs synchronously, or it would paint the light theme first", () => {
    // A bare <script>: no type="module" and no src, so it is neither deferred
    // nor fetched, and it executes before the document is painted.
    const inline = /<script>([\s\S]*?)<\/script>/.exec(indexHtml);

    expect(inline?.[1]).toContain(THEME_STORAGE_KEY);
  });

  it("guards the storage access, which throws where site data are blocked", () => {
    expect(indexHtml).toContain("try {");
    expect(indexHtml).toContain("catch (error)");
  });
});
