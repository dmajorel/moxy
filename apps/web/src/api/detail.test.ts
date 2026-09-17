import { afterEach, describe, expect, it, vi } from "vitest";

import {
  ApiParseError,
  ApiRequestError,
  fetchClusterSeries,
  fetchGuest,
  fetchGuestSeries,
  fetchGuestTasks,
  fetchNode,
  fetchNodeSeries,
  fetchTasks,
  guestPath,
  guestTasksPath,
  maintenancePath,
  nodePath,
  requestMaintenance,
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
    expect(guestTasksPath("prod", 101)).toBe("/api/clusters/prod/guests/101/tasks");
    expect(guestTasksPath("prod", 101, 10)).toBe(
      "/api/clusters/prod/guests/101/tasks?limit=10",
    );
  });
});

/** A node payload carrying everything the node view dereferences on sight. */
function nodeBody(patch: Record<string, unknown> = {}): Record<string, unknown> {
  return {
    name: "pve-01",
    status: "online",
    guests: [],
    cpu: { ratio: 0.31, cores: 32 },
    memory: { used: 1, total: 2, ratio: 0.5 },
    swap: { used: 0, total: 2, ratio: 0 },
    rootfs: { used: 1, total: 4, ratio: 0.25 },
    ...patch,
  };
}

/** The same, for a guest. */
function guestBody(patch: Record<string, unknown> = {}): Record<string, unknown> {
  return {
    vmid: 101,
    name: "sli-app-2601-qul",
    status: "running",
    cpu: { ratio: 0.02, cores: 4 },
    memory: { used: 1, total: 2, ratio: 0.5 },
    disk: { used: null, total: 4, ratio: null },
    tags: [],
    ...patch,
  };
}

describe("fetchNode", () => {
  it("requests the node route and returns the payload", async () => {
    const spy = stubFetch(respond(nodeBody()));

    const node = await fetchNode("prod", "pve-01");

    expect(spy.mock.calls[0]?.[0]).toBe("/api/clusters/prod/nodes/pve-01");
    expect(node.name).toBe("pve-01");
  });

  it("rejects a body that is not a node", async () => {
    stubFetch(respond({ totals: {} }));
    await expect(fetchNode("prod", "pve-01")).rejects.toBeInstanceOf(ApiParseError);
  });

  // These are the fields the node view dereferences on its first render:
  // node.cpu.ratio with no cpu is a TypeError thrown mid-render, which used to
  // unmount the whole application rather than degrade one screen.
  it.each(["cpu", "memory", "swap", "rootfs", "status"])(
    "rejects a body with no %s rather than crashing the screen",
    async (field) => {
      const body = nodeBody();
      delete body[field];
      stubFetch(respond(body));

      await expect(fetchNode("prod", "pve-01")).rejects.toBeInstanceOf(ApiParseError);
    },
  );

  it("rejects a cpu that is not a reading", async () => {
    stubFetch(respond(nodeBody({ cpu: { cores: 32 } })));
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
    const spy = stubFetch(respond(guestBody()));

    const guest = await fetchGuest("prod", 101);

    expect(spy.mock.calls[0]?.[0]).toBe("/api/clusters/prod/guests/101");
    expect(guest.vmid).toBe(101);
  });

  it.each(["cpu", "memory", "disk", "status", "tags"])(
    "rejects a body with no %s rather than crashing the screen",
    async (field) => {
      const body = guestBody();
      delete body[field];
      stubFetch(respond(body));

      await expect(fetchGuest("prod", 101)).rejects.toBeInstanceOf(ApiParseError);
    },
  );

  // An unmeasured boot disk reports a null used. That is a value the UI
  // renders as a dash, not a crash, so the guard must let it through.
  it("accepts a boot disk nothing measured", async () => {
    stubFetch(respond(guestBody({ disk: { used: null, total: 4, ratio: null } })));

    const guest = await fetchGuest("prod", 101);
    expect(guest.disk.used).toBeNull();
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

describe("requestMaintenance", () => {
  /** The shape moxyd answers a drain request with. */
  function result(patch: Record<string, unknown> = {}) {
    return {
      cluster: "prod",
      node: "pve-01",
      action: "enable",
      requestedAt: "2026-09-12T12:47:00Z",
      via: "pve-02",
      accepted: true,
      alreadyInState: false,
      output: "",
      ...patch,
    };
  }

  // The plan route stays a GET on `.../maintenance/plan`; this is a POST on
  // `.../maintenance`, one segment shorter, and confusing the two would turn a
  // read into a write.
  it("posts to the node's own maintenance route, not to the plan", () => {
    expect(maintenancePath("prod", "pve-01")).toBe(
      "/api/clusters/prod/nodes/pve-01/maintenance",
    );
    expect(maintenancePath("préprod", "a/b")).toBe(
      "/api/clusters/pr%C3%A9prod/nodes/a%2Fb/maintenance",
    );
  });

  it("sends the verb as JSON, which is what makes moxyd accept it", async () => {
    const spy = stubFetch(respond(result()));

    const answer = await requestMaintenance("prod", "pve-01", "enable");

    const [url, init] = spy.mock.calls[0] as [string, RequestInit];
    expect(url).toBe("/api/clusters/prod/nodes/pve-01/maintenance");
    expect(init.method).toBe("POST");
    expect(init.body).toBe(JSON.stringify({ action: "enable" }));
    // Without it moxyd answers 415: an HTML form on another site can post a
    // form encoding with no preflight, but never application/json.
    expect(new Headers(init.headers).get("Content-Type")).toBe("application/json");
    expect(answer.accepted).toBe(true);
    expect(answer.via).toBe("pve-02");
  });

  // The whole reason ApiRequestError grew a `kind`: a status code alone cannot
  // separate "no quorum" from "no HA manager", and the UI must not be reduced
  // to matching on English prose.
  it("carries the backend's kind through the failure", async () => {
    stubFetch(
      respond({ error: "cluster has no quorum", kind: "no_quorum" }, 409),
    );

    const error = await requestMaintenance("prod", "pve-01", "enable").catch(
      (cause: unknown) => cause,
    );

    expect(error).toBeInstanceOf(ApiRequestError);
    expect((error as ApiRequestError).status).toBe(409);
    expect((error as ApiRequestError).kind).toBe("no_quorum");
    expect((error as ApiRequestError).detail).toBe("cluster has no quorum");
  });

  // 400, 404 and 415 carry none, and a refusal with no kind is not a refusal
  // with the kind of the last one.
  it("leaves the kind null when the answer carries none", async () => {
    stubFetch(respond({ error: "malformed body" }, 400));

    const error = await requestMaintenance("prod", "pve-01", "enable").catch(
      (cause: unknown) => cause,
    );

    expect((error as ApiRequestError).kind).toBeNull();
  });

  it("reports a daemon that answered nothing at all", async () => {
    const spy = vi.fn((_input: RequestInfo | URL, _init?: RequestInit) =>
      Promise.reject(new TypeError("network")),
    );
    vi.stubGlobal("fetch", spy);

    const error = await requestMaintenance("prod", "pve-01", "disable").catch(
      (cause: unknown) => cause,
    );

    expect(error).toBeInstanceOf(ApiRequestError);
    expect((error as ApiRequestError).status).toBe(0);
  });

  it("rejects a 200 that is not a maintenance result", async () => {
    stubFetch(respond({ cluster: "prod", node: "pve-01" }));

    await expect(
      requestMaintenance("prod", "pve-01", "enable"),
    ).rejects.toBeInstanceOf(ApiParseError);
  });
});

describe("fetchGuestTasks", () => {
  it("asks the guest's own route rather than the cluster journal", async () => {
    const spy = stubFetch(respond({ cluster: "prod", entries: [{ upid: "UPID:x" }] }));

    const tasks = await fetchGuestTasks("prod", 101, 25);

    expect(spy.mock.calls[0]?.[0]).toBe(
      "/api/clusters/prod/guests/101/tasks?limit=25",
    );
    expect(tasks.entries).toHaveLength(1);
  });

  it("rejects a body without entries", async () => {
    stubFetch(respond({ cluster: "prod" }));
    await expect(fetchGuestTasks("prod", 101)).rejects.toBeInstanceOf(ApiParseError);
  });
});
