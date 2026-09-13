import { act, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { useNow } from "./useNow";

function setVisibility(state: DocumentVisibilityState) {
  Object.defineProperty(document, "visibilityState", {
    value: state,
    configurable: true,
  });
  document.dispatchEvent(new Event("visibilitychange"));
}

beforeEach(() => {
  vi.useFakeTimers();
});

afterEach(() => {
  vi.useRealTimers();
  Object.defineProperty(document, "visibilityState", {
    value: "visible",
    configurable: true,
  });
});

describe("useNow", () => {
  it("starts at the current instant", () => {
    const { result } = renderHook(() => useNow());
    expect(result.current).toBeInstanceOf(Date);
  });

  // "il y a 12 s" was computed at render and recomputed at the next one —
  // which, during an outage, is exactly what stops happening.
  it("ticks", () => {
    const { result } = renderHook(() => useNow(1000));
    const first = result.current;

    act(() => {
      vi.advanceTimersByTime(1000);
    });

    expect(result.current.getTime()).toBeGreaterThan(first.getTime());
  });

  it("honours the interval it is given", () => {
    const { result } = renderHook(() => useNow(60_000));
    const first = result.current;

    act(() => {
      vi.advanceTimersByTime(30_000);
    });
    expect(result.current).toBe(first);

    act(() => {
      vi.advanceTimersByTime(30_000);
    });
    expect(result.current.getTime()).toBeGreaterThan(first.getTime());
  });

  // Nothing is being read while the tab is hidden, so nothing needs
  // re-rendering — the same reason the polling stops.
  it("stops while the tab is hidden", () => {
    const { result } = renderHook(() => useNow(1000));

    act(() => {
      setVisibility("hidden");
    });
    const whenHidden = result.current;

    act(() => {
      vi.advanceTimersByTime(10_000);
    });

    expect(result.current).toBe(whenHidden);
  });

  // The label is as old as the time spent away, so it is brought up to date
  // before the next tick rather than after it.
  it("catches up the moment the tab comes back", () => {
    const { result } = renderHook(() => useNow(1000));

    act(() => {
      setVisibility("hidden");
    });
    const whenHidden = result.current;

    act(() => {
      vi.advanceTimersByTime(60_000);
      setVisibility("visible");
    });

    expect(result.current.getTime()).toBeGreaterThan(whenHidden.getTime());
  });

  it("resumes ticking after it comes back", () => {
    const { result } = renderHook(() => useNow(1000));

    act(() => {
      setVisibility("hidden");
      setVisibility("visible");
    });
    const resumed = result.current;

    act(() => {
      vi.advanceTimersByTime(1000);
    });

    expect(result.current.getTime()).toBeGreaterThan(resumed.getTime());
  });

  it("starts stopped when the tab is already hidden", () => {
    Object.defineProperty(document, "visibilityState", {
      value: "hidden",
      configurable: true,
    });

    const { result } = renderHook(() => useNow(1000));
    const first = result.current;

    act(() => {
      vi.advanceTimersByTime(10_000);
    });

    expect(result.current).toBe(first);
  });

  it("stops its timer on unmount", () => {
    const { unmount } = renderHook(() => useNow(1000));

    unmount();

    // A timer left behind would keep firing; nothing may be scheduled.
    expect(vi.getTimerCount()).toBe(0);
  });
});
