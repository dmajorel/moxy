import { StrictMode } from "react";
import { act, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { ApiRequestError } from "@/api/client";
import type { Overview } from "@/api/types";
import { POLL_INTERVAL_MS, useOverview } from "@/api/useOverview";

function makeOverview(vms: number): Overview {
  return {
    generatedAt: "2026-09-12T08:00:00Z",
    thresholds: { memory: 0.85 },
    totals: { clusters: 1, nodes: 2, nodesOnline: 2, vms, alerts: 0 },
    clusters: [],
  };
}

const first = makeOverview(7);
const second = makeOverview(9);

function jsonResponse(body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status: 200,
    headers: { "Content-Type": "application/json" },
  });
}

function failure(): Response {
  return new Response(JSON.stringify({ error: "overview unavailable" }), {
    status: 503,
    headers: { "Content-Type": "application/json" },
  });
}

interface Deferred<T> {
  promise: Promise<T>;
  resolve: (value: T) => void;
}

function deferred<T>(): Deferred<T> {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((settle) => {
    resolve = settle;
  });
  return { promise, resolve };
}

let fetchStub: ReturnType<typeof vi.fn>;

/** Signals handed to fetch, in call order, so aborts can be asserted. */
function signalOf(call: number): AbortSignal {
  const args = fetchStub.mock.calls[call] as [string, RequestInit] | undefined;
  if (args === undefined) {
    throw new Error(`fetch was not called ${String(call + 1)} times`);
  }
  const { signal } = args[1];
  if (!(signal instanceof AbortSignal)) {
    throw new Error("fetch was called without a signal");
  }
  return signal;
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
  fetchStub = vi.fn(() => Promise.resolve(jsonResponse(first)));
  vi.stubGlobal("fetch", fetchStub);
});

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

describe("useOverview", () => {
  it("loads on mount", async () => {
    const { result } = renderHook(() => useOverview());

    expect(result.current.isLoading).toBe(true);
    expect(result.current.data).toBeNull();

    await flush();

    expect(result.current.data).toEqual(first);
    expect(result.current.isLoading).toBe(false);
    expect(result.current.isStale).toBe(false);
    expect(result.current.error).toBeNull();
    expect(result.current.lastUpdatedAt).toBeInstanceOf(Date);
    expect(fetchStub).toHaveBeenCalledTimes(1);
  });

  it("polls again after the interval", async () => {
    fetchStub.mockImplementationOnce(() => Promise.resolve(jsonResponse(first)));
    fetchStub.mockImplementationOnce(() =>
      Promise.resolve(jsonResponse(second)),
    );
    const { result } = renderHook(() => useOverview());
    await flush();

    await advance(POLL_INTERVAL_MS);

    expect(fetchStub).toHaveBeenCalledTimes(2);
    expect(result.current.data).toEqual(second);
  });

  it("keeps the last reading and flags it stale when a poll fails", async () => {
    fetchStub.mockImplementationOnce(() => Promise.resolve(jsonResponse(first)));
    fetchStub.mockImplementationOnce(() => Promise.resolve(failure()));
    const { result } = renderHook(() => useOverview());
    await flush();
    const loadedAt = result.current.lastUpdatedAt;

    await advance(POLL_INTERVAL_MS);

    expect(result.current.data).toEqual(first);
    expect(result.current.isStale).toBe(true);
    expect(result.current.isLoading).toBe(false);
    expect(result.current.error).toBeInstanceOf(ApiRequestError);
    expect((result.current.error as ApiRequestError).status).toBe(503);
    expect(result.current.lastUpdatedAt).toBe(loadedAt);
  });

  it("reports a failing first attempt without staying in the loading state", async () => {
    fetchStub.mockImplementation(() => Promise.resolve(failure()));
    const { result } = renderHook(() => useOverview());

    await flush();

    expect(result.current.data).toBeNull();
    expect(result.current.isLoading).toBe(false);
    expect(result.current.isStale).toBe(false);
    expect(result.current.error).toBeInstanceOf(ApiRequestError);
  });

  it("clears the error once a later poll succeeds", async () => {
    fetchStub.mockImplementationOnce(() => Promise.resolve(jsonResponse(first)));
    fetchStub.mockImplementationOnce(() => Promise.resolve(failure()));
    fetchStub.mockImplementationOnce(() =>
      Promise.resolve(jsonResponse(second)),
    );
    const { result } = renderHook(() => useOverview());
    await flush();
    await advance(POLL_INTERVAL_MS);
    expect(result.current.isStale).toBe(true);

    await advance(POLL_INTERVAL_MS);

    expect(result.current.data).toEqual(second);
    expect(result.current.error).toBeNull();
    expect(result.current.isStale).toBe(false);
  });

  it("refreshes immediately and restarts the interval", async () => {
    const { result } = renderHook(() => useOverview());
    await flush();

    await act(async () => {
      result.current.refresh();
      await vi.advanceTimersByTimeAsync(0);
    });

    expect(fetchStub).toHaveBeenCalledTimes(2);

    // The cycle restarts from the manual refresh, not from the first load.
    await advance(POLL_INTERVAL_MS - 1);
    expect(fetchStub).toHaveBeenCalledTimes(2);
    await advance(1);
    expect(fetchStub).toHaveBeenCalledTimes(3);
  });

  it("never stacks requests when the backend is slower than the interval", async () => {
    const pending = deferred<Response>();
    fetchStub.mockImplementationOnce(() => pending.promise);
    const { result } = renderHook(() => useOverview());

    await advance(POLL_INTERVAL_MS * 3);

    expect(fetchStub).toHaveBeenCalledTimes(1);
    expect(result.current.isLoading).toBe(true);

    await act(async () => {
      pending.resolve(jsonResponse(first));
      await vi.advanceTimersByTimeAsync(0);
    });

    expect(result.current.data).toEqual(first);
  });

  it("aborts the in-flight request on unmount and writes no further state", async () => {
    const pending = deferred<Response>();
    fetchStub.mockImplementationOnce(() => pending.promise);
    const { result, unmount } = renderHook(() => useOverview());

    unmount();

    expect(signalOf(0).aborted).toBe(true);

    await act(async () => {
      pending.resolve(jsonResponse(first));
      await vi.advanceTimersByTimeAsync(POLL_INTERVAL_MS * 2);
    });

    expect(result.current.data).toBeNull();
    expect(fetchStub).toHaveBeenCalledTimes(1);
  });

  it("runs a single polling cycle under StrictMode", async () => {
    const { result } = renderHook(() => useOverview(), { wrapper: StrictMode });
    await flush();

    // The rehearsal mount is aborted by its own cleanup, never reported.
    expect(signalOf(0).aborted).toBe(true);
    expect(fetchStub).toHaveBeenCalledTimes(2);
    expect(result.current.data).toEqual(first);
    expect(result.current.error).toBeNull();

    await advance(POLL_INTERVAL_MS);
    expect(fetchStub).toHaveBeenCalledTimes(3);

    await advance(POLL_INTERVAL_MS);
    expect(fetchStub).toHaveBeenCalledTimes(4);
  });
});
