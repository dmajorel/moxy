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
  cpu: Cpu;
  memory: Usage;
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
  cpu: Cpu;
  memory: Usage;
  pendingUpdates: Unknown<number>;
  /** Guests hosted by this node. Absent until the backend reports them. */
  guests?: Guest[];
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
  | "unreachable";

export interface Alert {
  kind: AlertKind;
  nodes?: string[];
  ratio?: number;
  version?: string;
}
