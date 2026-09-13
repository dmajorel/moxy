import { describe, expect, it } from "vitest";

import clusterSeriesFixture from "@/test/fixtures/cluster-series.mock.json";
import guestFixture from "@/test/fixtures/guest.mock.json";
import guestSeriesFixture from "@/test/fixtures/guest-series.mock.json";
import guestTasksFixture from "@/test/fixtures/guest-tasks.mock.json";
import nodeFixture from "@/test/fixtures/node.mock.json";
import nodeUpdatesFixture from "@/test/fixtures/node-updates.mock.json";
import overviewFixture from "@/test/fixtures/overview.mock.json";
import planBlockedFixture from "@/test/fixtures/plan-blocked.mock.json";
import planFixture from "@/test/fixtures/plan.mock.json";
import seriesFixture from "@/test/fixtures/series.mock.json";
import tasksFixture from "@/test/fixtures/tasks.mock.json";

import type {
  Alert,
  Allocation,
  ApiError,
  ClusterOverview,
  Cpu,
  DiskUsage,
  Guest,
  GuestDetail,
  GuestDisk,
  MaintenancePlan,
  Node,
  NodeDetail,
  NodeUpdate,
  Overview,
  PlannedMove,
  Point,
  Quorum,
  Series,
  StayingGuest,
  TargetNode,
  Task,
  Tasks,
  Thresholds,
  Totals,
  Updates,
  Usage,
  VmCounts,
} from "./types";

/**
 * The other half of the Go ↔ TypeScript contract.
 *
 * apps/api/internal/server/fixtures_test.go generates every fixture in
 * src/test/fixtures from the mock daemon, on a pinned clock, and fails when
 * they are stale. That catches a Go field renamed without regenerating; it
 * cannot catch a Go field regenerated without following in this file, because
 * the JSON is valid either way.
 *
 * This is what catches that. Every check below runs in both directions:
 *
 *   - at compile time, assigning the fixture to its interface fails when this
 *     file declares a field the payload does not carry;
 *   - at run time, comparing the key sets fails when the payload carries a
 *     field this file does not declare.
 *
 * `satisfies Record<keyof T, true>` is what makes the second half work: it
 * forces the key list to be exhaustive, so a field added here without being
 * listed is a type error rather than a silent hole in the check.
 */

/** The keys of a payload, sorted, so two sets compare as text. */
function keysOf(value: object): string[] {
  return Object.keys(value).sort();
}

/**
 * Compares a declared key set against the one the payload actually carries.
 *
 * `optional` lists the fields Go marks `omitempty`: they are absent from the
 * JSON whenever they are empty, so their presence cannot be asserted — only
 * that nothing outside the declared set ever turns up.
 */
function expectSameShape(
  payload: object,
  declared: Record<string, true>,
  optional: readonly string[] = [],
) {
  const expected = new Set(Object.keys(declared));
  const actual = keysOf(payload);

  const unexpected = actual.filter((key) => !expected.has(key));
  expect(unexpected, "the payload carries fields types.ts does not declare").toEqual([]);

  const missing = [...expected]
    .filter((key) => !actual.includes(key))
    .filter((key) => !optional.includes(key));
  expect(missing, "types.ts declares fields the payload does not carry").toEqual([]);
}

/* -------------------------------------------------------------------------- *
 * Compile-time half: each fixture must be assignable to its interface.
 * -------------------------------------------------------------------------- */

/**
 * The shape a `.json` import can ever have.
 *
 * TypeScript widens every string of a JSON module to `string`, so a field
 * typed as a union of string literals — a status, a guest kind, a timeframe —
 * can never be assigned from one, however correct the value is. This widens
 * those, and only those: a missing field, a field the type does not declare,
 * or a number where a string belongs still fails to compile.
 */
type JsonOf<T> = T extends string
  ? string
  : T extends (infer Element)[]
    ? JsonOf<Element>[]
    : T extends object
      ? { [K in keyof T]: JsonOf<T[K]> }
      : T;

const overview: JsonOf<Overview> = overviewFixture;
const node: JsonOf<NodeDetail> = nodeFixture;
const nodeWithUpdates: JsonOf<NodeDetail> = nodeUpdatesFixture;
const guest: JsonOf<GuestDetail> = guestFixture;
const series: JsonOf<Series> = seriesFixture;
const guestSeries: JsonOf<Series> = guestSeriesFixture;
const clusterSeries: JsonOf<Series> = clusterSeriesFixture;
const tasks: JsonOf<Tasks> = tasksFixture;
const guestTasks: JsonOf<Tasks> = guestTasksFixture;
const plan: JsonOf<MaintenancePlan> = planFixture;
const blockedPlan: JsonOf<MaintenancePlan> = planBlockedFixture;

/* -------------------------------------------------------------------------- *
 * Run-time half: no field of the payload may be missing from types.ts.
 * -------------------------------------------------------------------------- */

const OVERVIEW_KEYS = {
  generatedAt: true,
  thresholds: true,
  totals: true,
  clusters: true,
} satisfies Record<keyof Overview, true>;

const THRESHOLDS_KEYS = { memory: true } satisfies Record<keyof Thresholds, true>;

const TOTALS_KEYS = {
  clusters: true,
  nodes: true,
  nodesOnline: true,
  vms: true,
  alerts: true,
} satisfies Record<keyof Totals, true>;

const CLUSTER_KEYS = {
  id: true,
  name: true,
  color: true,
  status: true,
  fetchedAt: true,
  error: true,
  quorum: true,
  cpu: true,
  memory: true,
  storage: true,
  vms: true,
  nodes: true,
  updates: true,
  alerts: true,
} satisfies Record<keyof ClusterOverview, true>;

const NODE_KEYS = {
  name: true,
  status: true,
  uptime: true,
  cpu: true,
  memory: true,
  pendingUpdates: true,
  guests: true,
} satisfies Record<keyof Node, true>;

const GUEST_KEYS = {
  vmid: true,
  name: true,
  kind: true,
  status: true,
  cpu: true,
  memory: true,
  tags: true,
} satisfies Record<keyof Guest, true>;

const CPU_KEYS = { ratio: true, cores: true } satisfies Record<keyof Cpu, true>;
const USAGE_KEYS = { used: true, total: true, ratio: true } satisfies Record<
  keyof Usage,
  true
>;
const DISK_USAGE_KEYS = { used: true, total: true, ratio: true } satisfies Record<
  keyof DiskUsage,
  true
>;
const QUORUM_KEYS = { quorate: true, nodes: true, online: true } satisfies Record<
  keyof Quorum,
  true
>;
const VM_COUNTS_KEYS = {
  running: true,
  stopped: true,
  templates: true,
  total: true,
} satisfies Record<keyof VmCounts, true>;

const UPDATES_KEYS = {
  nodes: true,
  pveManagerVersion: true,
  checkedAt: true,
} satisfies Record<keyof Updates, true>;

const API_ERROR_KEYS = { kind: true, status: true, message: true } satisfies Record<
  keyof ApiError,
  true
>;

const ALERT_KEYS = {
  kind: true,
  nodes: true,
  ratio: true,
  version: true,
  pendingMin: true,
  pendingMax: true,
} satisfies Record<keyof Alert, true>;

/** Everything an Alert carries beyond `kind` is `omitempty` on the Go side. */
const ALERT_OPTIONAL = ["nodes", "ratio", "version", "pendingMin", "pendingMax"];

const NODE_DETAIL_KEYS = {
  cluster: true,
  name: true,
  status: true,
  uptime: true,
  fetchedAt: true,
  pveVersion: true,
  kernelVersion: true,
  cpu: true,
  memory: true,
  swap: true,
  rootfs: true,
  loadAverage: true,
  quorum: true,
  haState: true,
  pendingUpdates: true,
  updates: true,
  guests: true,
} satisfies Record<keyof NodeDetail, true>;

const NODE_UPDATE_KEYS = {
  package: true,
  title: true,
  oldVersion: true,
  version: true,
} satisfies Record<keyof NodeUpdate, true>;

const GUEST_DETAIL_KEYS = {
  cluster: true,
  node: true,
  vmid: true,
  name: true,
  kind: true,
  status: true,
  uptime: true,
  fetchedAt: true,
  cpu: true,
  memory: true,
  disk: true,
  disks: true,
  allocated: true,
  hostMemory: true,
  tags: true,
  haState: true,
  ipv4: true,
} satisfies Record<keyof GuestDetail, true>;

const GUEST_DISK_KEYS = {
  key: true,
  storage: true,
  volume: true,
  size: true,
  attached: true,
} satisfies Record<keyof GuestDisk, true>;

const ALLOCATION_KEYS = {
  bytes: true,
  partial: true,
  detached: true,
  detachedBytes: true,
} satisfies Record<keyof Allocation, true>;

const SERIES_KEYS = {
  cluster: true,
  timeframe: true,
  fetchedAt: true,
  points: true,
  cpuAverage: true,
} satisfies Record<keyof Series, true>;

const POINT_KEYS = {
  time: true,
  cpu: true,
  memUsed: true,
  memTotal: true,
  netIn: true,
  netOut: true,
} satisfies Record<keyof Point, true>;

const TASKS_KEYS = { cluster: true, fetchedAt: true, entries: true } satisfies Record<
  keyof Tasks,
  true
>;

const TASK_KEYS = {
  upid: true,
  node: true,
  type: true,
  id: true,
  user: true,
  start: true,
  end: true,
  duration: true,
  status: true,
  outcome: true,
  warnings: true,
} satisfies Record<keyof Task, true>;

const PLAN_KEYS = {
  cluster: true,
  node: true,
  fetchedAt: true,
  threshold: true,
  feasible: true,
  moves: true,
  staying: true,
  targets: true,
  blockers: true,
} satisfies Record<keyof MaintenancePlan, true>;

const PLANNED_MOVE_KEYS = {
  vmid: true,
  name: true,
  kind: true,
  status: true,
  memory: true,
  method: true,
  ha: true,
  target: true,
  placed: true,
} satisfies Record<keyof PlannedMove, true>;

const STAYING_GUEST_KEYS = { vmid: true, name: true, reason: true } satisfies Record<
  keyof StayingGuest,
  true
>;

const TARGET_NODE_KEYS = {
  name: true,
  measured: true,
  before: true,
  after: true,
  incoming: true,
  exceeds: true,
} satisfies Record<keyof TargetNode, true>;

/** The first element of a list the fixture must not have left empty. */
function first<T>(list: T[], what: string): T {
  const head = list[0];
  if (head === undefined) {
    throw new Error(`the fixture carries no ${what}, so the contract is unchecked`);
  }
  return head;
}

describe("the overview payload matches types.ts", () => {
  it("at the top level", () => {
    expectSameShape(overview, OVERVIEW_KEYS);
    expectSameShape(overview.thresholds, THRESHOLDS_KEYS);
    expectSameShape(overview.totals, TOTALS_KEYS);
  });

  it("on every cluster, node and guest", () => {
    expect(overview.clusters.length).toBeGreaterThan(0);
    for (const cluster of overview.clusters) {
      expectSameShape(cluster, CLUSTER_KEYS);
      expectSameShape(cluster.storage, USAGE_KEYS);
      expectSameShape(cluster.vms, VM_COUNTS_KEYS);
      if (cluster.cpu !== null) expectSameShape(cluster.cpu, CPU_KEYS);
      if (cluster.memory !== null) expectSameShape(cluster.memory, USAGE_KEYS);
      if (cluster.quorum !== null) expectSameShape(cluster.quorum, QUORUM_KEYS);
      if (cluster.updates !== null) expectSameShape(cluster.updates, UPDATES_KEYS);
      if (cluster.error !== null) expectSameShape(cluster.error, API_ERROR_KEYS);

      for (const clusterNode of cluster.nodes) {
        expectSameShape(clusterNode, NODE_KEYS);
        if (clusterNode.cpu !== null) expectSameShape(clusterNode.cpu, CPU_KEYS);
        if (clusterNode.memory !== null) expectSameShape(clusterNode.memory, USAGE_KEYS);
        for (const clusterGuest of clusterNode.guests) {
          expectSameShape(clusterGuest, GUEST_KEYS);
          expectSameShape(clusterGuest.cpu, CPU_KEYS);
          expectSameShape(clusterGuest.memory, USAGE_KEYS);
        }
      }
    }
  });

  // The sample data exercises every alert kind, and each carries a different
  // subset of the optional fields.
  it("on every alert, whichever optional fields it carries", () => {
    const alerts = overview.clusters.flatMap((cluster) => cluster.alerts);
    expect(alerts.length).toBeGreaterThan(0);
    for (const alert of alerts) {
      expectSameShape(alert, ALERT_KEYS, ALERT_OPTIONAL);
    }
  });

  // An error and a cluster that could not be read at all are the states the
  // mock had to grow a fourth cluster to produce; if that one ever goes away,
  // this says so rather than passing on a payload that exercises nothing.
  it("on the unreachable cluster, which is the one carrying an error", () => {
    const failing = overview.clusters.filter((cluster) => cluster.error !== null);
    expect(failing.length, "no cluster in the sample carries an error").toBeGreaterThan(0);
  });
});

describe("the node payload matches types.ts", () => {
  it("on a node whose updates could not be asked for", () => {
    expectSameShape(node, NODE_DETAIL_KEYS);
    expectSameShape(node.cpu, CPU_KEYS);
    expectSameShape(node.memory, USAGE_KEYS);
    expectSameShape(node.swap, USAGE_KEYS);
    expectSameShape(node.rootfs, USAGE_KEYS);
    if (node.quorum !== null) expectSameShape(node.quorum, QUORUM_KEYS);
    for (const nodeGuest of node.guests) expectSameShape(nodeGuest, GUEST_KEYS);
  });

  it("on a node that lists pending packages", () => {
    expectSameShape(nodeWithUpdates, NODE_DETAIL_KEYS);
    const updates = nodeWithUpdates.updates ?? [];
    expect(updates.length, "the fixture lists no pending package").toBeGreaterThan(0);
    for (const update of updates) expectSameShape(update, NODE_UPDATE_KEYS);
  });
});

describe("the guest payload matches types.ts", () => {
  it("with its volumes and its allocation", () => {
    expectSameShape(guest, GUEST_DETAIL_KEYS);
    expectSameShape(guest.cpu, CPU_KEYS);
    expectSameShape(guest.memory, USAGE_KEYS);
    expectSameShape(guest.disk, DISK_USAGE_KEYS);
    if (guest.allocated !== null) expectSameShape(guest.allocated, ALLOCATION_KEYS);
    const disks = guest.disks ?? [];
    expect(disks.length, "the fixture declares no volume").toBeGreaterThan(0);
    for (const disk of disks) expectSameShape(disk, GUEST_DISK_KEYS);
  });
});

describe("the series payloads match types.ts", () => {
  // Three shapes behind one type: a node's own series, a guest's, and the one
  // folded from every node of a cluster. The third carries no network columns,
  // so a check on the first alone would not prove the type covers it.
  it.each([
    ["node", series],
    ["guest", guestSeries],
    ["cluster", clusterSeries],
  ])("on the %s series", (_what, payload) => {
    expectSameShape(payload, SERIES_KEYS);
    expect(payload.points.length).toBeGreaterThan(0);
    for (const point of payload.points) expectSameShape(point, POINT_KEYS);
  });
});

describe("the task payloads match types.ts", () => {
  it.each([
    ["cluster journal", tasks],
    ["guest history", guestTasks],
  ])("on the %s", (_what, payload) => {
    expectSameShape(payload, TASKS_KEYS);
    expect(payload.entries.length).toBeGreaterThan(0);
    for (const entry of payload.entries) expectSameShape(entry, TASK_KEYS);
  });

  // The four outcomes are the reason `outcome` is a string and not a boolean;
  // a fixture that only ever succeeds would leave three of them unchecked.
  it("exercises more than one outcome", () => {
    const outcomes = new Set(tasks.entries.map((entry) => entry.outcome));
    expect(outcomes.size).toBeGreaterThan(1);
  });
});

describe("the maintenance plan matches types.ts", () => {
  it.each([
    ["a drain that fits", plan],
    ["a drain that is blocked", blockedPlan],
  ])("on %s", (_what, payload) => {
    expectSameShape(payload, PLAN_KEYS);
    for (const move of payload.moves) expectSameShape(move, PLANNED_MOVE_KEYS);
    for (const staying of payload.staying) expectSameShape(staying, STAYING_GUEST_KEYS);
    for (const target of payload.targets) {
      expectSameShape(target, TARGET_NODE_KEYS);
      expectSameShape(target.before, USAGE_KEYS);
      expectSameShape(target.after, USAGE_KEYS);
    }
  });

  it("carries at least one move to check", () => {
    expect(first(plan.moves, "planned move").vmid).toBeGreaterThan(0);
  });

  // Blockers are stable keys, not sentences: the UI maps them to labels, so an
  // unknown one would render as raw backend vocabulary.
  it("states its blockers as keys", () => {
    expect(blockedPlan.feasible).toBe(false);
    expect(blockedPlan.blockers.length).toBeGreaterThan(0);
    for (const blocker of blockedPlan.blockers) {
      expect(blocker).toMatch(/^[a-z][a-z0-9_]*$/);
    }
  });
});
