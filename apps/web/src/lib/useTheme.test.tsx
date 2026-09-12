import { act, renderHook } from "@testing-library/react";

import { stubBrokenLocalStorage, stubMatchMedia } from "@/test/stubs";
import { THEME_ATTRIBUTE, THEME_STORAGE_KEY } from "@/lib/theme";

import { useTheme } from "./useTheme";

afterEach(() => {
  window.localStorage.clear();
  document.documentElement.removeAttribute(THEME_ATTRIBUTE);
});

describe("useTheme", () => {
  it("starts on the system preference when nothing was ever chosen", () => {
    const { result } = renderHook(() => useTheme());

    expect(result.current.preference).toBe("system");
    expect(document.documentElement.hasAttribute(THEME_ATTRIBUTE)).toBe(false);
  });

  it("restores the choice of a previous visit", () => {
    window.localStorage.setItem(THEME_STORAGE_KEY, "dark");

    const { result } = renderHook(() => useTheme());

    expect(result.current.preference).toBe("dark");
    expect(result.current.resolvedTheme).toBe("dark");
    expect(document.documentElement.getAttribute(THEME_ATTRIBUTE)).toBe("dark");
  });

  it("stamps the document when the preference changes", () => {
    const { result } = renderHook(() => useTheme());

    act(() => {
      result.current.setPreference("dark");
    });

    expect(document.documentElement.getAttribute(THEME_ATTRIBUTE)).toBe("dark");
    expect(result.current.resolvedTheme).toBe("dark");
  });

  it("persists the choice for the next visit", () => {
    const { result } = renderHook(() => useTheme());

    act(() => {
      result.current.setPreference("light");
    });

    expect(window.localStorage.getItem(THEME_STORAGE_KEY)).toBe("light");
  });

  it("forgets the choice when going back to the system preference", () => {
    window.localStorage.setItem(THEME_STORAGE_KEY, "dark");
    const { result } = renderHook(() => useTheme());

    act(() => {
      result.current.setPreference("system");
    });

    expect(window.localStorage.getItem(THEME_STORAGE_KEY)).toBeNull();
    expect(document.documentElement.hasAttribute(THEME_ATTRIBUTE)).toBe(false);
  });

  it("still applies the theme when storage refuses to keep it", () => {
    const restore = stubBrokenLocalStorage();
    try {
      const { result } = renderHook(() => useTheme());

      act(() => {
        result.current.setPreference("dark");
      });

      expect(result.current.preference).toBe("dark");
      expect(document.documentElement.getAttribute(THEME_ATTRIBUTE)).toBe("dark");
    } finally {
      restore();
    }
  });

  it("resolves to dark on a dark desktop, without stamping the attribute", () => {
    const media = stubMatchMedia(true);
    try {
      const { result } = renderHook(() => useTheme());

      expect(result.current.preference).toBe("system");
      expect(result.current.resolvedTheme).toBe("dark");
      // The media query owns this case: stamping would freeze it.
      expect(document.documentElement.hasAttribute(THEME_ATTRIBUTE)).toBe(false);
    } finally {
      media.restore();
    }
  });

  it("follows the desktop switching over while on the system preference", () => {
    const media = stubMatchMedia(false);
    try {
      const { result } = renderHook(() => useTheme());
      expect(result.current.resolvedTheme).toBe("light");

      act(() => {
        media.setMatches(true);
      });

      expect(result.current.resolvedTheme).toBe("dark");
    } finally {
      media.restore();
    }
  });

  it("ignores the desktop switching over once a choice is explicit", () => {
    const media = stubMatchMedia(false);
    try {
      const { result } = renderHook(() => useTheme());

      act(() => {
        result.current.setPreference("light");
      });
      act(() => {
        media.setMatches(true);
      });

      expect(result.current.resolvedTheme).toBe("light");
      expect(document.documentElement.getAttribute(THEME_ATTRIBUTE)).toBe("light");
    } finally {
      media.restore();
    }
  });

  it("detaches its media listener when it stops following the system", () => {
    const media = stubMatchMedia(false);
    try {
      const { result } = renderHook(() => useTheme());
      expect(media.listenerCount()).toBe(1);

      act(() => {
        result.current.setPreference("dark");
      });

      expect(media.listenerCount()).toBe(0);
    } finally {
      media.restore();
    }
  });

  it("renders without a matchMedia at all", () => {
    expect(typeof window.matchMedia).toBe("undefined");

    const { result } = renderHook(() => useTheme());

    expect(result.current.resolvedTheme).toBe("light");
  });
});
