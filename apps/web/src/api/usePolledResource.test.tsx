import { useCallback } from "react";
import { act, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import {
  MAX_POLL_INTERVAL_MS,
  POLL_INTERVAL_MS,
  usePolledResource,
} from "@/api/usePolledResource";

/**
 * The cadence, the backoff and the hidden tab are exercised through useOverview,
 * which is where an operator meets them. What is checked here is what only the
 * generic hook knows: that a new fetcher is a new subject, that an explicit
 * refresh supersedes a request still in flight, and that a rejection which is
 * not an Error still becomes one.
 */

interface Deferred<T> {
  promise: Promise<T>;
  resolve: (value: T) => void;
  reject: (cause: unknown) => void;
}

function deferred<T>(): Deferred<T> {
  let resolve!: (value: T) => void;
  let reject!: (cause: unknown) => void;
  const promise = new Promise<T>((settle, fail) => {
    resolve = settle;
    reject = fail;
  });
  return { promise, resolve, reject };
}

/**
 * A fetcher keyed on its subject, the way every caller builds one: a new
 * identity is how the hook is told it is watching something else.
 */
function useFetcherFor(value: string, seen?: string[]): () => Promise<string> {
  return useCallback(() => {
    seen?.push(value);
    return Promise.resolve(value);
  }, [value, seen]);
}

/**
 * Rejects with whatever it is given, Error or not.
 *
 * The rule is right in general and wrong here: what is under test is precisely
 * that the hook survives a rejection which is not an Error, which is what a
 * `throw "..."` deep in a dependency produces.
 */
function rejectWith(cause: unknown): Promise<string> {
  // eslint-disable-next-line @typescript-eslint/prefer-promise-reject-errors
  return Promise.reject(cause);
}

/** Lets pending promises settle without moving the clock forward. */
async function flush(): Promise<void> {
  await act(async () => {
    await vi.advanceTimersByTimeAsync(0);
  });
}

async function advance(ms: number): Promise<void> {
  await act(async () => {
    await vi.advanceTimersByTimeAsync(ms);
  });
}

beforeEach(() => {
  vi.useFakeTimers();
});

afterEach(() => {
  vi.useRealTimers();
});

describe("usePolledResource", () => {
  it("reports the very first attempt as loading, and only it", async () => {
    // Stable identity, as the hook requires of every caller: a fetcher built
    // inline would be a new subject on every render.
    const fetcher = () => Promise.resolve("a");
    const { result } = renderHook(() => usePolledResource(fetcher));

    expect(result.current.isLoading).toBe(true);
    expect(result.current.isStale).toBe(false);

    await flush();

    expect(result.current.data).toBe("a");
    expect(result.current.isLoading).toBe(false);
    expect(result.current.lastUpdatedAt).toBeInstanceOf(Date);
  });

  // A fetcher doubles as the identity of what is being watched: showing the
  // previous node's figures under the new heading would be worse than showing
  // nothing at all.
  it("drops the reading when the subject changes", async () => {
    const { result, rerender } = renderHook(
      ({ value }: { value: string }) =>
        usePolledResource(useFetcherFor(value), POLL_INTERVAL_MS),
      { initialProps: { value: "a" } },
    );
    await flush();
    expect(result.current.data).toBe("a");

    rerender({ value: "b" });

    // Before the answer arrives: no data, and loading again.
    expect(result.current.data).toBeNull();
    expect(result.current.isLoading).toBe(true);

    await flush();
    expect(result.current.data).toBe("b");
  });

  it("keeps polling the new subject, not the old one", async () => {
    const seen: string[] = [];
    const { rerender } = renderHook(
      ({ value }: { value: string }) => {
        const fetcher = useFetcherFor(value, seen);
        return usePolledResource(fetcher, POLL_INTERVAL_MS);
      },
      { initialProps: { value: "a" } },
    );
    await flush();

    rerender({ value: "b" });
    await flush();
    await advance(POLL_INTERVAL_MS);

    expect(seen).toEqual(["a", "b", "b"]);
  });

  // A scheduled tick never stacks on a slow backend, but the operator who
  // presses "Réessayer" is not made to wait for the request that is hanging.
  it("supersedes an in-flight request when the operator refreshes", async () => {
    const first = deferred<string>();
    const signals: AbortSignal[] = [];
    const fetcher = vi.fn((signal: AbortSignal) => {
      signals.push(signal);
      return signals.length === 1 ? first.promise : Promise.resolve("second");
    });
    const { result } = renderHook(() => usePolledResource(fetcher));

    act(() => {
      result.current.refresh();
    });

    expect(signals[0]?.aborted).toBe(true);
    expect(fetcher).toHaveBeenCalledTimes(2);

    // The superseded answer, arriving late, is not the one displayed.
    await act(async () => {
      first.resolve("first");
      await vi.advanceTimersByTimeAsync(0);
    });

    expect(result.current.data).toBe("second");
  });

  it("ignores a scheduled tick while a request is still in flight", async () => {
    const pending = deferred<string>();
    const fetcher = vi.fn(() => pending.promise);
    renderHook(() => usePolledResource(fetcher));

    await advance(POLL_INTERVAL_MS * 3);

    expect(fetcher).toHaveBeenCalledTimes(1);
  });

  it("honours the cadence it is given rather than the default", async () => {
    const fetcher = vi.fn(() => Promise.resolve("a"));
    renderHook(() => usePolledResource(fetcher, MAX_POLL_INTERVAL_MS));
    await flush();

    await advance(POLL_INTERVAL_MS * 2);
    expect(fetcher).toHaveBeenCalledTimes(1);

    await advance(MAX_POLL_INTERVAL_MS);
    expect(fetcher).toHaveBeenCalledTimes(2);
  });

  // A rejection that is not an Error would reach explainError as a string and
  // leave the screen with no message at all.
  it("reports a rejection that is not an Error as one", async () => {
    const fetcher = () => rejectWith("upstream said no");
    const { result } = renderHook(() => usePolledResource(fetcher));

    await flush();

    expect(result.current.error).toBeInstanceOf(Error);
    expect(result.current.error?.message).toBe("upstream said no");
  });

  it("aborts on unmount and writes nothing afterwards", async () => {
    const pending = deferred<string>();
    const signals: AbortSignal[] = [];
    const fetcher = (signal: AbortSignal) => {
      signals.push(signal);
      return pending.promise;
    };
    const { result, unmount } = renderHook(() => usePolledResource(fetcher));

    unmount();
    expect(signals[0]?.aborted).toBe(true);

    await act(async () => {
      pending.resolve("late");
      await vi.advanceTimersByTimeAsync(POLL_INTERVAL_MS * 2);
    });

    expect(result.current.data).toBeNull();
  });

  // An abort is not a failure: it must neither be shown nor counted towards
  // the backoff, since whoever aborted decides what comes next.
  it("does not report an abort as an error", async () => {
    const fetcher = () => rejectWith(new DOMException("aborted", "AbortError"));
    const { result } = renderHook(() => usePolledResource(fetcher));

    await flush();

    expect(result.current.error).toBeNull();
    // Still the very first attempt, so still loading: nothing was answered.
    expect(result.current.isLoading).toBe(true);
  });
});
