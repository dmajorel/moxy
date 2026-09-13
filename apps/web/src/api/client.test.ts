import { afterEach, describe, expect, it, vi } from "vitest";

import {
  ApiParseError,
  ApiRequestError,
  HEALTH_PATH,
  OVERVIEW_PATH,
  fetchHealth,
  fetchOverview,
} from "@/api/client";
import type { Overview } from "@/api/types";

const overview: Overview = {
  generatedAt: "2026-09-12T08:00:00Z",
  thresholds: { memory: 0.85, cpu: 0.85, storage: 0.85 },
  totals: { clusters: 1, nodes: 2, nodesOnline: 2, vms: 7, alerts: 0 },
  clusters: [],
};

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

describe("fetchOverview", () => {
  it("decodes a successful response", async () => {
    const stub = stubFetch(() => Promise.resolve(jsonResponse(overview)));

    await expect(fetchOverview()).resolves.toEqual(overview);
    expect(stub).toHaveBeenCalledTimes(1);
    const [url, init] = stub.mock.calls[0] as [string, RequestInit];
    expect(url).toBe(OVERVIEW_PATH);
    expect(init.method).toBe("GET");
  });

  it("reports the status and the backend message of a failed response", async () => {
    stubFetch(() =>
      Promise.resolve(jsonResponse({ error: "overview unavailable" }, 503)),
    );

    const error = await fetchOverview().catch((cause: unknown) => cause);

    expect(error).toBeInstanceOf(ApiRequestError);
    const failure = error as ApiRequestError;
    expect(failure.status).toBe(503);
    expect(failure.detail).toBe("overview unavailable");
    expect(failure.message).toContain("503");
    expect(failure.message).toContain("overview unavailable");
  });

  it("reports the status alone when the failed body carries no message", async () => {
    stubFetch(() =>
      Promise.resolve(new Response("<html>bad gateway</html>", { status: 502 })),
    );

    const error = await fetchOverview().catch((cause: unknown) => cause);

    expect(error).toBeInstanceOf(ApiRequestError);
    expect((error as ApiRequestError).status).toBe(502);
    expect((error as ApiRequestError).detail).toBeNull();
  });

  it("rejects with a clear error when the body is not JSON", async () => {
    stubFetch(() =>
      Promise.resolve(new Response("<!doctype html><title>proxy</title>")),
    );

    const error = await fetchOverview().catch((cause: unknown) => cause);

    expect(error).toBeInstanceOf(ApiParseError);
    expect((error as Error).message).toContain("not valid JSON");
  });

  it("rejects with a clear error when the JSON is not an overview", async () => {
    stubFetch(() => Promise.resolve(jsonResponse({ hello: "world" })));

    const error = await fetchOverview().catch((cause: unknown) => cause);

    expect(error).toBeInstanceOf(ApiParseError);
    expect((error as Error).message).toContain("not an overview");
  });

  it("rejects with a clear error when the body cannot be read", async () => {
    const unreadable = {
      ok: true,
      status: 200,
      text: () => Promise.reject(new Error("stream closed")),
    } as unknown as Response;
    stubFetch(() => Promise.resolve(unreadable));

    const error = await fetchOverview().catch((cause: unknown) => cause);

    expect(error).toBeInstanceOf(ApiParseError);
    expect((error as Error).message).toContain("could not be read");
  });

  it("reports a transport failure without a status", async () => {
    stubFetch(() => Promise.reject(new TypeError("Failed to fetch")));

    const error = await fetchOverview().catch((cause: unknown) => cause);

    expect(error).toBeInstanceOf(ApiRequestError);
    expect((error as ApiRequestError).status).toBe(0);
  });

  it("forwards the abort signal and propagates the abort untouched", async () => {
    const controller = new AbortController();
    const stub = stubFetch((_input, init) => {
      init?.signal?.throwIfAborted();
      return Promise.resolve(jsonResponse(overview));
    });

    controller.abort();
    const error = await fetchOverview(controller.signal).catch(
      (cause: unknown) => cause,
    );

    const [, init] = stub.mock.calls[0] as [string, RequestInit];
    expect(init.signal).toBe(controller.signal);
    expect((error as Error).name).toBe("AbortError");
    expect(error).not.toBeInstanceOf(ApiRequestError);
  });
});

describe("fetchHealth", () => {
  it("decodes the liveness answer", async () => {
    const stub = stubFetch(() =>
      Promise.resolve(jsonResponse({ status: "ok", version: "0862b0c" })),
    );

    await expect(fetchHealth()).resolves.toEqual({
      status: "ok",
      version: "0862b0c",
    });
    const [url] = stub.mock.calls[0] as [string, RequestInit];
    expect(url).toBe(HEALTH_PATH);
  });

  it("rejects a body that is not a health answer", async () => {
    // What a proxy answering /healthz with something else of its own looks
    // like, and what keeps such a payload from reaching the bar as a version.
    stubFetch(() => Promise.resolve(jsonResponse({ status: "ok" })));

    await expect(fetchHealth()).rejects.toBeInstanceOf(ApiParseError);
  });

  it("reports the status of a failed response", async () => {
    stubFetch(() => Promise.resolve(jsonResponse({ error: "nope" }, 503)));

    const error = await fetchHealth().catch((cause: unknown) => cause);

    expect(error).toBeInstanceOf(ApiRequestError);
    expect((error as ApiRequestError).status).toBe(503);
    expect((error as ApiRequestError).message).toContain(HEALTH_PATH);
  });
});
