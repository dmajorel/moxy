import { StrictMode } from "react";
import { renderHook, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { useHealth } from "@/api/useHealth";

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

function stubFetch(impl: typeof fetch): ReturnType<typeof vi.fn> {
  const stub = vi.fn(impl);
  vi.stubGlobal("fetch", stub);
  return stub;
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("useHealth", () => {
  it("returns the answer of the single request it makes", async () => {
    const stub = stubFetch(() =>
      Promise.resolve(jsonResponse({ status: "ok", version: "0862b0c" })),
    );

    const { result } = renderHook(() => useHealth());

    await waitFor(() => {
      expect(result.current).toEqual({ status: "ok", version: "0862b0c" });
    });
    expect(stub).toHaveBeenCalledTimes(1);
    const [url] = stub.mock.calls[0] as [string];
    expect(url).toBe("/healthz");
  });

  it("stays unknown and reports nothing when the request fails", async () => {
    const stub = stubFetch(() =>
      Promise.resolve(jsonResponse({ error: "nope" }, 503)),
    );

    const { result } = renderHook(() => useHealth());

    // The failure must be swallowed rather than left to reject: an unhandled
    // rejection here would fail the run, which is why it is exercised at all.
    await waitFor(() => {
      expect(stub).toHaveBeenCalledTimes(1);
    });
    expect(result.current).toBeNull();
  });

  it("writes no state after unmount, StrictMode's rehearsal included", async () => {
    const stub = stubFetch(() =>
      Promise.resolve(jsonResponse({ status: "ok", version: "0862b0c" })),
    );

    const { result, unmount } = renderHook(() => useHealth(), {
      wrapper: StrictMode,
    });
    unmount();

    await waitFor(() => {
      expect(stub).toHaveBeenCalled();
    });
    expect(result.current).toBeNull();
  });
});
