import { afterEach, describe, expect, it, vi } from "vitest";

import {
  ApiParseError,
  ApiRequestError,
  fetchClusterSeries,
  fetchGuest,
  fetchGuestSeries,
  fetchNode,
  fetchNodeSeries,
  fetchTasks,
  guestPath,
  nodePath,
  tasksPath,
} from "./client";

function respond(body: unknown, status = 200): Response {
  return new Response(typeof body === "string" ? body : JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

function stubFetch(response: Response) {
  // Typed with fetch's own signature so mock.calls carries the requested URL;
  // a bare vi.fn(() => ...) types its calls as an empty tuple.
  const spy = vi.fn((_input: RequestInfo | URL, _init?: RequestInit) =>
    Promise.resolve(response),
  );
  vi.stubGlobal("fetch", spy);
  return spy;
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("path building", () => {
  it("percent-encodes every segment", () => {
    // A name holding a slash or an accent must not become a different route:
    // the backend decodes and validates the segments itself.
    expect(nodePath("préprod", "pve 01")).toBe(
      "/api/clusters/pr%C3%A9prod/nodes/pve%2001",
    );
    expect(nodePath("prod", "a/b")).toBe("/api/clusters/prod/nodes/a%2Fb");
    expect(guestPath("prod", 101)).toBe("/api/clusters/prod/guests/101");
  });

  it("omits the limit when none is asked for", () => {
    expect(tasksPath("prod")).toBe("/api/clusters/prod/tasks");
    expect(tasksPath("prod", 10)).toBe("/api/clusters/prod/tasks?limit=10");
  });
});

describe("fetchNode", () => {
  it("requests the node route and returns the payload", async () => {
    const spy = stubFetch(respond({ name: "pve-01", guests: [] }));

    const node = await fetchNode("prod", "pve-01");

    expect(spy.mock.calls[0]?.[0]).toBe("/api/clusters/prod/nodes/pve-01");
    expect(node.name).toBe("pve-01");
  });

  it("rejects a body that is not a node", async () => {
    stubFetch(respond({ totals: {} }));
    await expect(fetchNode("prod", "pve-01")).rejects.toBeInstanceOf(ApiParseError);
  });

  it("names the failing path, not the overview", async () => {
    stubFetch(respond({ error: "not found" }, 404));

    const failure = await fetchNode("prod", "ghost").catch((e: unknown) => e);

    expect(failure).toBeInstanceOf(ApiRequestError);
    expect((failure as ApiRequestError).status).toBe(404);
    expect((failure as Error).message).toContain("/api/clusters/prod/nodes/ghost");
    expect((failure as Error).message).not.toContain("/api/overview");
  });
});

describe("fetchGuest", () => {
  it("returns the payload", async () => {
    const spy = stubFetch(respond({ vmid: 101, name: "sli-app-2601-qul" }));

    const guest = await fetchGuest("prod", 101);

    expect(spy.mock.calls[0]?.[0]).toBe("/api/clusters/prod/guests/101");
    expect(guest.vmid).toBe(101);
  });

  it("rejects a body missing the vmid", async () => {
    stubFetch(respond({ name: "x" }));
    await expect(fetchGuest("prod", 101)).rejects.toBeInstanceOf(ApiParseError);
  });
});

describe("series", () => {
  it("passes the timeframe for a node", async () => {
    const spy = stubFetch(respond({ points: [], cpuAverage: 0 }));

    await fetchNodeSeries("prod", "pve-01", "day");

    expect(spy.mock.calls[0]?.[0]).toBe(
      "/api/clusters/prod/nodes/pve-01/rrd?timeframe=day",
    );
  });

  it("passes the timeframe for a guest", async () => {
    const spy = stubFetch(respond({ points: [], cpuAverage: 0 }));

    await fetchGuestSeries("prod", 101, "hour");

    expect(spy.mock.calls[0]?.[0]).toBe(
      "/api/clusters/prod/guests/101/rrd?timeframe=hour",
    );
  });

  it("asks the cluster route for a whole cluster", async () => {
    const spy = stubFetch(respond({ points: [], cpuAverage: 0 }));

    await fetchClusterSeries("pprd", "hour");

    expect(spy.mock.calls[0]?.[0]).toBe("/api/clusters/pprd/rrd?timeframe=hour");
  });

  it("keeps null samples intact", async () => {
    // RRD returns gaps; turning them into 0 would draw a drop that never
    // happened, which is precisely what the fixed-height chart must avoid.
    stubFetch(
      respond({
        points: [{ time: "2026-09-12T10:00:00Z", cpu: null, memUsed: null }],
        cpuAverage: 0.004,
      }),
    );

    const series = await fetchNodeSeries("prod", "pve-01", "hour");

    expect(series.points[0]?.cpu).toBeNull();
    expect(series.cpuAverage).toBeCloseTo(0.004);
  });

  it("rejects a body without points", async () => {
    stubFetch(respond({ cpuAverage: 0 }));
    await expect(fetchNodeSeries("prod", "pve-01", "hour")).rejects.toBeInstanceOf(
      ApiParseError,
    );
  });
});

describe("fetchTasks", () => {
  it("returns the entries", async () => {
    stubFetch(respond({ cluster: "prod", entries: [{ upid: "UPID:x" }] }));

    const tasks = await fetchTasks("prod", 7);

    expect(tasks.entries).toHaveLength(1);
  });

  it("rejects a body without entries", async () => {
    stubFetch(respond({ cluster: "prod" }));
    await expect(fetchTasks("prod")).rejects.toBeInstanceOf(ApiParseError);
  });
});
