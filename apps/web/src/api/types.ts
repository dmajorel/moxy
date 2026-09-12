/**
 * Mirror of the Go model in apps/api/internal/aggregate/model.go.
 *
 * The backend serves raw data on purpose: byte counts, not GiB, and ratios in
 * [0,1], not percentages. Formatting — units, rounding, the French decimal
 * comma — is this layer's job, so that no localized string is ever baked into
 * the API. Keep this file in step with model.go; it is the contract.
 */

/** null means unknown, which is never the same as zero. */
export type Unknown<T> = T | null;

export interface Overview {
  generatedAt: string;
  thresholds: Thresholds;
  totals: Totals;
  clusters: ClusterOverview[];
}

export interface Thresholds {
  memory: number;
}

export interface Totals {
  clusters: number;
  nodes: number;
  nodesOnline: number;
  vms: number;
  alerts: number;
}

export type ClusterStatus = "healthy" | "degraded" | "unreachable";

export interface ClusterOverview {
  id: string;
  name: string;
  color: Unknown<string>;
  status: ClusterStatus;
  /** null while no poll has ever succeeded. */
  fetchedAt: Unknown<string>;
  error: Unknown<ApiError>;
  /** null for a standalone node, which has no quorum. */
  quorum: Unknown<Quorum>;
  /**
   * null when no node reported any figure, typically because the token lacks
   * Sys.Audit on /nodes. Unknown is never served as zero.
   */
  cpu: Unknown<Cpu>;
  memory: Unknown<Usage>;
  /** Shared capacity usable for guest disks, every Ceph storage counted once. */
  storage: Usage;
  vms: VmCounts;
  nodes: Node[];
  /** null when the token may not ask for pending updates. */
  updates: Unknown<Updates>;
  alerts: Alert[];
}

/** kind is for the UI to translate; message stays in English. */
export interface ApiError {
  kind: string;
  message: string;
}

export interface Quorum {
  quorate: boolean;
  nodes: number;
  online: number;
}

export interface Cpu {
  ratio: number;
  cores: number;
}

/** Sizes are byte counts. */
export interface Usage {
  used: number;
  total: number;
  ratio: number;
}

/** Templates are counted apart and excluded from total. */
export interface VmCounts {
  running: number;
  stopped: number;
  templates: number;
  total: number;
}

export type NodeStatus = "online" | "offline" | "maintenance" | "unknown";

export interface Node {
  name: string;
  status: NodeStatus;
  /** Seconds. */
  uptime: number;
  /**
   * null when /cluster/resources listed the node without figures, which is
   * what PVE does when the token may not audit it. Not zero: the node may be
   * perfectly busy, we simply cannot see it.
   */
  cpu: Unknown<Cpu>;
  memory: Unknown<Usage>;
  pendingUpdates: Unknown<number>;
  /** Guests hosted by this node, sorted by VMID. Never null. */
  guests: Guest[];
}

export type GuestKind = "qemu" | "lxc";
export type GuestStatus = "running" | "stopped" | "template";

export interface Guest {
  vmid: number;
  name: string;
  kind: GuestKind;
  status: GuestStatus;
  cpu: Cpu;
  memory: Usage;
  tags: string[];
}

export interface Updates {
  nodes: string[];
  pveManagerVersion: Unknown<string>;
  checkedAt: string;
}

export type AlertKind =
  | "quorum_lost"
  | "node_offline"
  | "memory_high"
  | "updates_available"
  | "unreachable"
  | "node_stats_unavailable";

export interface Alert {
  kind: AlertKind;
  nodes?: string[];
  ratio?: number;
  version?: string;
}

/* -------------------------------------------------------------------------- *
 * Detail API — mirror of apps/api/internal/detail/model.go.
 *
 * The overview is polled because it is always on screen; these per-object views
 * are fetched on demand and cached briefly by the backend. Same conventions
 * throughout: bytes, ratios in [0,1], and null meaning "unknown", never zero.
 * -------------------------------------------------------------------------- */

/** RRD window accepted by the series endpoints. */
export type Timeframe = "hour" | "day" | "week" | "month" | "year";

export interface NodeDetail {
  cluster: string;
  name: string;
  /** Same vocabulary as the overview: the two views must never disagree. */
  status: NodeStatus;
  /** Seconds. */
  uptime: number;
  fetchedAt: string;
  pveVersion: Unknown<string>;
  kernelVersion: Unknown<string>;
  cpu: Cpu;
  memory: Usage;
  swap: Usage;
  rootfs: Usage;
  /** The 1, 5 and 15 minute figures, in that order. */
  loadAverage: Unknown<[number, number, number]>;
  /** null for a standalone node, which has no quorum. */
  quorum: Unknown<Quorum>;
  /** The CRM's own word for this node; null when the cluster runs no HA. */
  haState: Unknown<string>;
  pendingUpdates: Unknown<number>;
  guests: Guest[];
}

export interface GuestDetail {
  cluster: string;
  /** Changes on migration, so it is read from the payload, never assumed. */
  node: string;
  vmid: number;
  name: string;
  kind: GuestKind;
  status: GuestStatus;
  /** Seconds. */
  uptime: number;
  fetchedAt: string;
  cpu: Cpu;
  memory: Usage;
  /** Boot disk. Its `used` is often zero: only a guest agent reports it. */
  disk: Usage;
  /** What the hypervisor spends on this guest, above what the guest sees. */
  hostMemory: Unknown<number>;
  tags: string[];
  haState: Unknown<string>;
  /** From the guest agent; null without it. */
  ipv4: Unknown<string>;
}

/**
 * One RRD sample. Every metric is nullable because RRD returns gaps, and a gap
 * filled with 0 would draw a drop that never happened.
 */
export interface Point {
  time: string;
  cpu: Unknown<number>;
  memUsed: Unknown<number>;
  memTotal: Unknown<number>;
  netIn: Unknown<number>;
  netOut: Unknown<number>;
}

export interface Series {
  cluster: string;
  timeframe: Timeframe;
  fetchedAt: string;
  points: Point[];
  /** Mean CPU ratio over the window, shown as a label beside the chart. */
  cpuAverage: number;
}

export interface Task {
  upid: string;
  node: string;
  type: string;
  id: string;
  user: string;
  start: string;
  /** null while the task is still running. */
  end: Unknown<string>;
  /** Seconds, computed by the backend so the UI never subtracts timestamps. */
  duration: Unknown<number>;
  /** "running", "OK", or the raw PVE error string. */
  status: string;
  ok: Unknown<boolean>;
}

export interface Tasks {
  cluster: string;
  fetchedAt: string;
  entries: Task[];
}

/* -------------------------------------------------------------------------- *
 * Maintenance plan — mirror of apps/api/internal/detail/plan.go.
 *
 * Strictly read-only: it says what draining a node would entail and whether the
 * cluster has room, and changes nothing. moxy cannot perform the drain — PVE
 * exposes no REST route for node maintenance.
 * -------------------------------------------------------------------------- */

export interface MaintenancePlan {
  cluster: string;
  node: string;
  fetchedAt: string;
  /** Memory share each target must stay under, as a fraction. */
  threshold: number;
  /** False as soon as one guest cannot be placed within the threshold. */
  feasible: boolean;
  moves: PlannedMove[];
  staying: StayingGuest[];
  targets: TargetNode[];
  /** Stable keys, not sentences: no_target, source_offline. */
  blockers: string[];
}

export interface PlannedMove {
  vmid: number;
  name: string;
  kind: GuestKind;
  status: GuestStatus;
  /** The figure the capacity check used; zero for a stopped guest. */
  memory: number;
  /** Empty when nowhere could take this guest. */
  target: string;
  placed: boolean;
}

export interface StayingGuest {
  vmid: number;
  name: string;
  /** Stable key: template. */
  reason: string;
}

export interface TargetNode {
  name: string;
  before: Usage;
  after: Usage;
  incoming: number;
  /** Already over the threshold; informational, it does not block the plan. */
  exceeds: boolean;
}
