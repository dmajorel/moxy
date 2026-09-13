/**
 * Display formatting for the moxy UI.
 *
 * The backend serves raw data only — byte counts, seconds, ratios in [0,1] — so
 * every localized string in the product is built here. Rendering is done by
 * hand rather than through `Intl`/`toLocaleString`: ICU output varies between
 * Node builds (notably which space it puts before a `%`), and these strings are
 * asserted character by character in the tests and in the mockups of appendix A
 * of `docs/PROXMOX_UI_HANDOFF.md`.
 *
 * Typographic conventions:
 *   - decimal comma, as French requires (`1,2 TiB`, never `1.2 TiB`);
 *   - U+202F NARROW NO-BREAK SPACE before `%` and as the thousands separator.
 *     U+202F is the modern French standard for both and is what current ICU
 *     emits for `fr-FR`; a plain space is not used because a line break between
 *     a number and its `%`, or in the middle of `1 024`, is a rendering bug;
 *   - a plain space between a number and any other unit (`61 GiB`, `47 min`),
 *     matching the mockups;
 *   - no function throws: an aberrant input (NaN, Infinity, negative) renders
 *     the em dash fallback, which reads as "unknown" in the UI.
 */

import type {
  Alert,
  ApiError,
  Allocation,
  ClusterStatus,
  DiskUsage,
  GuestKind,
  GuestStatus,
  NodeStatus,
  Quorum,
  TaskOutcome,
  Usage,
} from "@/api/types";

/** U+202F, narrow no-break space: before `%` and between thousands groups. */
export const NNBSP = " ";

/** Rendered for any value we cannot express: NaN, Infinity, negative sizes. */
export const FALLBACK = "—";

const BYTE_UNITS = ["o", "KiB", "MiB", "GiB", "TiB", "PiB"] as const;

function isUsableNumber(value: number | null | undefined): value is number {
  return typeof value === "number" && Number.isFinite(value);
}

/**
 * Rounds half away from zero, which is what a reader expects from a size
 * (`0.5` renders `1`), unlike the banker-ish drift of repeated float ops.
 */
function roundTo(value: number, digits: number): number {
  const factor = 10 ** digits;
  return Math.round(Math.abs(value) * factor) / factor * Math.sign(value || 1);
}

/**
 * Renders a finite number with at most `digits` decimals, French style.
 * Trailing zeros are dropped: a decimal is shown only when it carries
 * information, so 61.0 renders `61` and 1.2 renders `1,2`.
 */
function formatNumber(value: number, digits: number): string {
  const negative = value < 0;
  const fixed = Math.abs(roundTo(value, digits)).toFixed(Math.max(0, digits));
  let [integer = "0", fraction = ""] = fixed.split(".");
  fraction = fraction.replace(/0+$/, "");
  const grouped = integer.replace(/\B(?=(\d{3})+(?!\d))/g, NNBSP);
  const body = fraction ? `${grouped},${fraction}` : grouped;
  return negative ? `-${body}` : body;
}

interface ScaledBytes {
  /** Value expressed in `unit`. */
  value: number;
  unit: string;
  /** Decimals to display for this magnitude. */
  digits: number;
  /** Index in `BYTE_UNITS`, so a caller can express another value here too. */
  exponent: number;
}

/**
 * Expresses a byte count in an imposed unit, with the precision rule below:
 * no decimal at or above 100, one decimal under it, raw bytes for `o`.
 */
function scaleAt(bytes: number, exponent: number): ScaledBytes {
  const value = bytes / 1024 ** exponent;
  return {
    value,
    unit: BYTE_UNITS[exponent] ?? "o",
    digits: exponent === 0 || value >= 100 ? 0 : 1,
    exponent,
  };
}

/**
 * Picks the binary unit and the precision for a byte count.
 *
 * Precision rule, taken from the mockups (`1,2 TiB`, `61 GiB`, `418 GiB`,
 * `28 GiB`): at or above 100 the decimal is noise, so none is shown; below 100
 * one decimal is computed and kept only when it is significant. A value that
 * rounds up to 1024 is promoted to the next unit so that 1023.7 GiB renders
 * `1 TiB` and never `1 024 GiB`.
 */
function scaleBytes(bytes: number): ScaledBytes {
  let exponent = 0;
  let value = bytes;
  const last = BYTE_UNITS.length - 1;
  while (value >= 1024 && exponent < last) {
    value /= 1024;
    exponent += 1;
  }
  // Bytes are counted, never fractional; larger units get one decimal below 100.
  let digits = exponent === 0 || value >= 100 ? 0 : 1;
  if (roundTo(value, digits) >= 1024 && exponent < last) {
    value /= 1024;
    exponent += 1;
    digits = value >= 100 ? 0 : 1;
  }
  return { value, unit: BYTE_UNITS[exponent] ?? "o", digits, exponent };
}

/**
 * Renders a byte count with its binary unit: `1,2 TiB`, `61 GiB`, `418 GiB`.
 * Negative, NaN and Infinity render the fallback; 0 renders `0 o`.
 */
export function formatBytes(bytes: number): string {
  if (!isUsableNumber(bytes) || bytes < 0) return FALLBACK;
  const scaled = scaleBytes(bytes);
  return `${formatNumber(scaled.value, scaled.digits)} ${scaled.unit}`;
}

/**
 * Renders a used/total pair with the unit written once: `212 / 256 GiB`,
 * `418 / 1 024 GiB`.
 *
 * Both values share one unit so that the fill level can be judged at a glance.
 * `418 GiB / 1 TiB` forces a mental conversion before the reader knows whether
 * the node is half full — exactly the friction this UI exists to remove — so
 * the total is expressed in the used value's magnitude when they differ, and
 * `1 024 GiB` is preferred to `1 TiB`. This is what the four cluster cards of
 * appendix A.4 show.
 *
 * The shared unit is the one of the total, stepped down by **one** unit when
 * the used value would otherwise read below 1 in it. One step is the whole
 * rule: it covers every pair within a factor of 1024 of each other, which is
 * every realistic used/total pair.
 *
 * Exception — when one step is not enough, the two values are more than a unit
 * apart and no shared unit can hold both: the used value would round to 0
 * (`0 / 8 TiB`, a lie) or the total would need seven digits
 * (`980 / 8 388 608 MiB`, unreadable). Separate units are then the honest
 * rendering: `980 MiB / 8 TiB`.
 *
 * A used value of exactly 0 keeps the shared unit — `0 / 8 TiB` is true and
 * reads fine — and an aberrant or empty total renders the fallback.
 */
export function formatUsage(usage: Usage | DiskUsage | null | undefined): string {
  if (!usage || !isUsableNumber(usage.used) || !isUsableNumber(usage.total)) {
    return FALLBACK;
  }
  const { used } = usage;
  if (used < 0 || usage.total <= 0) return FALLBACK;

  const separate = () => `${formatBytes(used)} / ${formatBytes(usage.total)}`;

  let exponent = scaleBytes(usage.total).exponent;
  if (used > 0) {
    if (used / 1024 ** exponent < 1 && exponent > 0) exponent -= 1;
    if (used / 1024 ** exponent < 1) return separate();
  }

  const scaled = scaleAt(used, exponent);
  const total = scaleAt(usage.total, exponent);
  // Safety net: never let a non-zero usage render as a flat 0.
  if (used > 0 && roundTo(scaled.value, scaled.digits) === 0) return separate();

  const usedText = formatNumber(scaled.value, scaled.digits);
  const totalText = formatNumber(total.value, total.digits);
  return `${usedText} / ${totalText} ${total.unit}`;
}

/**
 * Renders a ratio in [0,1] as a percentage: 0.31 → `31 %`, 0.828 → `83 %`,
 * 0.0025 → `0,25 %`.
 *
 * Without an explicit `digits`, precision adapts to the magnitude the way the
 * mockups do — whole percents above 10 (`31 %`), one decimal in between
 * (`3,1 %`), two below one percent (`0,25 %`) — and trailing zeros are dropped,
 * so 0.04 renders `4 %` and not `4,0 %`.
 *
 * A non-zero ratio never renders `0 %`, which would read as "nothing": below
 * the smallest representable value it renders `< 0,1 %` (or `< 1 %` when the
 * caller pinned `digits` to 0). Exactly 0 renders `0 %`, and an unknown ratio
 * (`null`) renders the fallback: the backend sends null, never a fake zero.
 */
export function formatRatio(ratio: number | null | undefined, digits?: number): string {
  if (ratio === null || ratio === undefined || !isUsableNumber(ratio) || ratio < 0) {
    return FALLBACK;
  }
  if (digits !== undefined && (!isUsableNumber(digits) || digits < 0)) {
    return FALLBACK;
  }

  const percent = ratio * 100;
  if (percent === 0) return `0${NNBSP}%`;

  if (digits === undefined) {
    // Below 0,1 % the UI says so explicitly rather than inventing decimals.
    if (percent < 0.1) return `<${NNBSP}0,1${NNBSP}%`;
    const adaptive = percent >= 10 ? 0 : percent >= 1 ? 1 : 2;
    return `${formatNumber(percent, adaptive)}${NNBSP}%`;
  }

  const pinned = Math.min(Math.trunc(digits), 6);
  if (roundTo(percent, pinned) === 0) {
    const smallest = formatNumber(10 ** -pinned, pinned);
    return `<${NNBSP}${smallest}${NNBSP}%`;
  }
  return `${formatNumber(percent, pinned)}${NNBSP}%`;
}

/**
 * Renders a processor count with its unit: `32 c`, `1 024 c`.
 *
 * The abbreviation is the one the mockups write next to a CPU load, as in
 * `3,1 % · 32 c`, and the count is the one PVE reports in `maxcpu`: logical
 * processors, threads included. Guests are measured in `vCPU` instead and do
 * not go through here.
 *
 * A count of zero renders the fallback rather than a machine with no
 * processor: PVE lists a node without `maxcpu` when it may not be audited,
 * and that is an unknown, not a measurement.
 */
export function formatCores(cores: number | null | undefined): string {
  if (cores === null || cores === undefined) return FALLBACK;
  if (!isUsableNumber(cores) || cores <= 0) return FALLBACK;
  return `${formatNumber(cores, 0)} c`;
}

/**
 * The vCPU count of a guest: `4 vCPU`.
 *
 * "vCPU" is invariable here, as PVE writes it. A zero is not a guest with no
 * processor — PVE always assigns at least one — it is a figure that did not
 * arrive, so it renders the dash.
 */
export function formatVcpus(cores: number | null | undefined): string {
  if (cores === null || cores === undefined) return FALLBACK;
  if (!isUsableNumber(cores) || cores <= 0) return FALLBACK;
  return `${formatNumber(cores, 0)} vCPU`;
}

/**
 * The three load average figures, in the order the kernel reports them:
 * `0,84 · 0,91 · 0,88`.
 *
 * It goes through formatNumber like every other figure of the UI, so that the
 * decimal comma, the thousands separator and the rounding are decided in one
 * place. A toFixed written in a screen would drift the day any of the three
 * changes, silently and only there.
 *
 * A genuine `0,00 0,00 0,00` IS shown: a quiet node really reports it, and
 * turning that into "unknown" would be the symmetrical lie. Only an absent or
 * unusable reading renders the dash.
 */
export function formatLoadAverage(
  load: readonly [number, number, number] | null | undefined,
): string {
  if (load === null || load === undefined || load.length !== 3) return FALLBACK;
  if (load.some((value) => !isUsableNumber(value) || value < 0)) return FALLBACK;
  return load.map((value) => formatNumber(value, 2)).join(" · ");
}

/**
 * Renders a duration in seconds as at most two units, largest first:
 * `41 j`, `2 j 22 h`, `3 h 14 min`, `47 min`, `12 s`.
 *
 * The second unit is the one immediately below the first and is written only
 * when non-zero, so 1 day and 1 minute reads `1 j`, never `1 j 0 h` nor a
 * misleading `1 j 1 min`.
 */
export function formatUptime(seconds: number | null): string {
  // null is "no uptime to report": an offline node, a stopped guest, a node
  // the token may not audit. The em dash says so; a "0 s" would claim the
  // machine came up this very second.
  if (!isUsableNumber(seconds) || seconds < 0) return FALLBACK;

  const total = Math.floor(seconds);
  const parts = [
    { value: Math.floor(total / 86400), unit: "j" },
    { value: Math.floor(total / 3600) % 24, unit: "h" },
    { value: Math.floor(total / 60) % 60, unit: "min" },
    { value: total % 60, unit: "s" },
  ];

  const first = parts.findIndex((part) => part.value > 0);
  if (first === -1) return "0 s";

  const head = parts[first];
  if (!head) return "0 s";
  const tail = parts[first + 1];
  const text = `${head.value} ${head.unit}`;
  return tail && tail.value > 0 ? `${text} ${tail.value} ${tail.unit}` : text;
}

/**
 * Renders how long ago `date` happened, used for data freshness:
 * `à l'instant`, `il y a 12 s`, `il y a 3 min`, `il y a 2 h`, `il y a 3 j`.
 *
 * A single unit is enough here — the reader wants staleness, not a duration.
 * Anything under five seconds, and any date in the future (clock skew between
 * the browser and the hypervisors is routine), reads `à l'instant`.
 */
export function formatRelativeTime(date: Date, now: Date = new Date()): string {
  if (!(date instanceof Date) || Number.isNaN(date.getTime())) return FALLBACK;
  if (!(now instanceof Date) || Number.isNaN(now.getTime())) return FALLBACK;

  const seconds = Math.floor((now.getTime() - date.getTime()) / 1000);
  if (seconds < 5) return "à l'instant";
  if (seconds < 60) return `il y a ${seconds} s`;
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `il y a ${minutes} min`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `il y a ${hours} h`;
  return `il y a ${Math.floor(hours / 24)} j`;
}

/**
 * Full name of a guest, as PVE spells it. The only way moxy names a guest.
 *
 * The name is never shortened and never carries the vmid. It is what one reads,
 * searches for and copies, so it has to survive as the real string rather than
 * a rewrite of it; and every surface that shows a guest — the tree, the node's
 * table, the maintenance plan — already carries the id in its own right, in a
 * dedicated column or not at all.
 *
 * Overflow is a layout concern, left to the caller: the tree truncates in CSS
 * and puts the whole name in the row's tooltip, the tables let their wrapper
 * scroll. Neither case changes the string.
 *
 * A guest without a name falls back to its vmid, which is degraded but still
 * identifies the row; with neither, the em dash.
 */
export function formatGuestName(vmid: number, name: string): string {
  const clean = typeof name === "string" ? name.trim() : "";
  if (clean) return clean;
  return isUsableNumber(vmid) ? String(Math.trunc(vmid)) : FALLBACK;
}

/**
 * One Proxmox tag, read as the key/value pair the convention of a fleet makes
 * it: `ha.state.started` is the state `started` of `ha.state`, not one opaque
 * word.
 *
 * The cut is at the LAST dot, never the first and never a `split(".")`: the
 * key is the namespace, however many levels it has, and the value is the one
 * segment that qualifies it. Cutting at the first dot would file
 * `ha.state.started` under `ha` with a value of `state.started`, which is a
 * different — and wrong — reading of the same string.
 *
 * PVE imposes no structure at all, so a tag need not be a pair. `production`
 * is a flag: it names without qualifying, and comes back whole as the key with
 * a null value, which the key/value panel renders as the em dash. A tag whose
 * cut would leave either half empty — `.foo`, `foo.` — is read the same way:
 * an amputated row would claim a pair that is not there.
 */
export function splitTag(tag: string): { key: string; value: string | null } {
  const cut = tag.lastIndexOf(".");
  if (cut <= 0 || cut === tag.length - 1) return { key: tag, value: null };
  return { key: tag.slice(0, cut), value: tag.slice(cut + 1) };
}

const NODE_STATUS_LABELS: Record<NodeStatus, string> = {
  online: "En ligne",
  offline: "Hors ligne",
  maintenance: "Maintenance",
  unknown: "Inconnu",
};

/** French sentence-case label of a node status. */
export function formatNodeStatus(status: NodeStatus): string {
  return NODE_STATUS_LABELS[status] ?? "Inconnu";
}

/**
 * State of a guest, in the ONE word this interface uses for it.
 *
 * "template", "Template" and "Modèle" all named the same state in different
 * places, which reads as three states. French, sentence case, like every other
 * label: `Modèle`.
 */
const GUEST_STATUS_LABELS: Record<GuestStatus, string> = {
  running: "En cours",
  stopped: "Arrêtée",
  template: "Modèle",
};

export function formatGuestStatus(status: GuestStatus): string {
  return GUEST_STATUS_LABELS[status] ?? "Inconnu";
}

/**
 * What kind of guest this is, spelled out: a container is not a virtual
 * machine, and calling it one is how a breadcrumb ends up reading "VM 105"
 * about an LXC.
 */
const GUEST_KIND_LABELS: Record<GuestKind, string> = {
  qemu: "Machine virtuelle",
  lxc: "Conteneur LXC",
};

export function formatGuestKind(kind: GuestKind): string {
  return GUEST_KIND_LABELS[kind] ?? "Invité";
}

/**
 * The short designation of a guest: `VM 103`, `CT 105`.
 *
 * "CT" is PVE's own abbreviation for a container, which is what an operator
 * reads in the native interface and in `pct`. A breadcrumb calling an LXC
 * "VM 105" contradicts every other tool they use.
 */
export function formatGuestRef(kind: GuestKind, vmid: number): string {
  const prefix = kind === "lxc" ? "CT" : "VM";
  if (!isUsableNumber(vmid)) return prefix;
  return `${prefix} ${String(Math.trunc(vmid))}`;
}

/**
 * The quorum line of a node: `OK · 3/3 votes`, `Perdu · 1/3 votes`, and
 * `Nœud seul` for a machine that belongs to no cluster.
 *
 * A standalone node has no quorum at all — inventing a one-node vote would put
 * a healthy machine in a state it is not in — which is why null is a sentence
 * of its own rather than the dash.
 */
export function formatQuorum(quorum: Quorum | null | undefined): string {
  if (quorum === null || quorum === undefined) return "Nœud seul";
  const { quorate, online, nodes } = quorum;
  if (!isUsableNumber(online) || !isUsableNumber(nodes)) return FALLBACK;
  const verdict = quorate ? "OK" : "Perdu";
  return `${verdict} · ${formatNumber(online, 0)}/${formatNumber(nodes, 0)} votes`;
}

/**
 * `12/09/2026 à 14:32`, the stamp of the staleness banner.
 *
 * Built by hand rather than through Intl for the reason stated at the top of
 * this file: ICU output drifts between Node builds, and these strings are
 * asserted character by character. Null for a date that cannot be read, so the
 * caller writes a sentence with no timestamp rather than "Invalid Date".
 */
export function formatDateTime(date: Date | null | undefined): string | null {
  if (!(date instanceof Date) || Number.isNaN(date.getTime())) {
    return null;
  }
  const pad = (value: number) => String(value).padStart(2, "0");
  const day = `${pad(date.getDate())}/${pad(date.getMonth() + 1)}/${String(date.getFullYear())}`;
  return `${day} à ${pad(date.getHours())}:${pad(date.getMinutes())}`;
}

/** A count, grouped the way every other figure of the UI is: `1 024`. */
export function formatInteger(value: number): string {
  if (!isUsableNumber(value)) return FALLBACK;
  return formatNumber(Math.trunc(value), 0);
}

/**
 * `1 nœud` / `3 nœuds`. French pluralises from 2, so a zero takes the plural.
 *
 * It lives here rather than in the three screens that each had their own copy:
 * the rule is typographic, and typography is decided once.
 */
export function plural(count: number, singular: string, pluralForm: string): string {
  if (!isUsableNumber(count)) return FALLBACK;
  const n = Math.trunc(count);
  return `${formatNumber(n, 0)} ${n === 1 ? singular : pluralForm}`;
}

/**
 * The value of the "Mises à jour" row of a node: `À jour` when nothing is
 * pending, `12 en attente` otherwise, and null when the question could not be
 * asked — the caller renders that as the dash, never as a reassuring zero.
 */
export function formatPendingUpdates(pending: number | null): string | null {
  if (pending === null) return null;
  // "En attente" is invariable here: the count carries the plural.
  return pending === 0 ? "À jour" : `${String(pending)} en attente`;
}

/** `1 paquet` / `12 paquets`, the subtitle of the pending-updates table. */
export function formatPackageCount(count: number): string {
  return count === 1 ? "1 paquet" : `${String(count)} paquets`;
}

/** `1 disque` / `4 disques`, the subtitle of the volume table. */
export function formatDiskCount(count: number): string {
  return count === 1 ? "1 disque" : `${String(count)} disques`;
}

/**
 * The qualifier next to a guest's allocated volumetry.
 *
 * `· alloué` when every attached volume declares a size, `· au moins` when one
 * does not: the total is then a floor, and saying so is the whole point — a
 * bare number would claim a precision the configuration does not carry. Null
 * when the configuration could not be read at all, which the caller renders as
 * the em dash rather than as a total of zero.
 */
export function formatAllocationQualifier(
  allocation: Allocation | null,
): string | null {
  if (allocation === null) return null;
  return allocation.partial ? "· au moins" : "· alloué";
}

/**
 * The note under the volume table about what is not counted in the total:
 * `1 volume détaché (8 GiB)`, `3 volumes détachés`, and null when there is
 * none — nothing to say, no line to draw.
 *
 * A detached volume still occupies its storage, so hiding it would understate
 * what the guest costs; counting it in the total would overstate what the
 * guest uses. It is reported, apart.
 */
export function formatDetachedVolumes(allocation: Allocation | null): string | null {
  if (allocation === null || allocation.detached === 0) return null;
  const label =
    allocation.detached === 1
      ? "1 volume détaché"
      : `${String(allocation.detached)} volumes détachés`;
  // PVE records no size for an unused volume, so this is usually unknown.
  return allocation.detachedBytes === 0
    ? label
    : `${label} (${formatBytes(allocation.detachedBytes)})`;
}

/**
 * How a pending package's versions are written: `257.3-1 → 257.4-1`, or the
 * new version alone for a package apt would install for the first time, which
 * reports no old version.
 */
export function formatVersionChange(
  oldVersion: string | null,
  version: string,
): string {
  return oldVersion === null ? version : `${oldVersion} → ${version}`;
}

const CLUSTER_STATUS_LABELS: Record<ClusterStatus, string> = {
  healthy: "Sain",
  degraded: "Dégradé",
  unreachable: "Injoignable",
};

/** French sentence-case label of a cluster status. */
/**
 * The CRM's vocabulary for an HA resource or an HA node, in French.
 *
 * The backend serves the manager's own word — `started`, `fence`, `migrate` —
 * because that is what an operator needs to read during an incident. Anything
 * the list does not cover is shown as it came: a state invented by a newer PVE
 * is still more useful raw than hidden behind "inconnu".
 */
const HA_STATE_LABELS: Record<string, string> = {
  started: "Démarré",
  stopped: "Arrêté",
  disabled: "Désactivé",
  ignored: "Ignoré",
  error: "Erreur",
  fence: "Isolation",
  freeze: "Gelé",
  migrate: "Migration",
  relocate: "Relocalisation",
  // The node states of the same endpoint.
  online: "Actif",
  maintenance: "En maintenance",
  unknown: "Inconnu",
  gone: "Disparu",
};

export function formatHaState(state: string | null | undefined): string | null {
  if (state === null || state === undefined || state.trim() === "") return null;
  return HA_STATE_LABELS[state] ?? state;
}

/**
 * Why a cluster could not be read, in French.
 *
 * The backend classifies every failure and says so in `kind`, then leaves the
 * wording here — that is the whole contract. Leaving it untranslated meant the
 * card said "Lecture ancienne · il y a 12 min" for a revoked token, which sends
 * an operator looking at the network for something that is a two-minute fix.
 */
export function formatErrorKind(error: ApiError | null | undefined): string | null {
  if (!error || typeof error.kind !== "string") return null;
  switch (error.kind) {
    case "auth":
      // 401 and 403 are both "auth" upstream and mean opposite errands: one is
      // a token that is no longer valid, the other a token missing a privilege.
      if (error.status === 401) return "jeton refusé";
      if (error.status === 403) return "droits insuffisants";
      return "authentification refusée";
    case "tls":
      return "certificat non vérifiable";
    case "timeout":
      return "délai dépassé";
    case "network":
      return "réseau injoignable";
    case "protocol":
      return "réponse inattendue";
    default:
      return error.kind;
  }
}

export function formatClusterStatus(status: ClusterStatus): string {
  return CLUSTER_STATUS_LABELS[status] ?? "Inconnu";
}

/** `2 nœuds` / `1 nœud`, or an empty string when the count is unknown. */
function nodeCount(nodes: string[] | undefined): string {
  if (!Array.isArray(nodes) || nodes.length === 0) return "";
  return nodes.length === 1 ? "1 nœud" : `${nodes.length} nœuds`;
}

/**
 * Builds the banner sentence of an alert, as shown on the cluster cards:
 * `Mémoire à 83 % sur 2 nœuds (max.)`, `Mise à jour 9.2.12 disponible sur
 * 5 nœuds`, `Quorum perdu`, `2 nœuds hors ligne`, `1 nœud dans un état
 * inconnu`, `Cluster injoignable`.
 *
 * Both the singular and the plural are handled, and every optional field
 * (`ratio`, `version`, `nodes`) degrades to a shorter but still grammatical
 * sentence when the backend could not fill it in.
 */
export function formatAlert(alert: Alert): string {
  if (!alert || typeof alert.kind !== "string") return "Alerte";
  const count = nodeCount(alert.nodes);
  const on = count ? ` sur ${count}` : "";

  switch (alert.kind) {
    case "quorum_lost":
      return "Quorum perdu";
    case "unreachable":
      return "Cluster injoignable";
    case "node_offline":
      return count ? `${count} hors ligne` : "Nœud hors ligne";
    case "node_unknown":
      // Not "hors ligne": the cluster never said this node is down, it simply
      // never mentioned it. A node that has just joined reads like this for a
      // few seconds, and a stale resource row of a departed node for as long
      // as PVE keeps it.
      return count
        ? `${count} dans un état inconnu`
        : "Nœud dans un état inconnu";
    case "memory_high": {
      const ratio =
        alert.ratio !== undefined && isUsableNumber(alert.ratio) && alert.ratio >= 0
          ? formatRatio(alert.ratio)
          : "";
      if (!ratio) return `Mémoire élevée${on}`;
      // With nodes named, the ratio is the highest of theirs, not the cluster
      // average -- "(max.)" says which figure this is, so that a single node
      // at 92 % in a cluster at 55 % reads as the node it is about.
      return count ? `Mémoire à ${ratio}${on} (max.)` : `Mémoire à ${ratio}`;
    }
    case "updates_available": {
      const version = alert.version ? ` ${alert.version}` : "";
      return `Mise à jour${version} disponible${on}`;
    }
    case "updates_uneven": {
      // No "sur N nœuds" suffix here: the alert is about the spread between the
      // nodes, not about a set of them. It degrades to the bare sentence when
      // the bounds are missing, like the other kinds carrying optional fields.
      const { pendingMin: min, pendingMax: max } = alert;
      const bounded =
        min !== undefined &&
        max !== undefined &&
        isUsableNumber(min) &&
        isUsableNumber(max) &&
        min >= 0 &&
        max > min;
      return bounded
        ? `Mises à jour inégales : de ${min} à ${max} paquets en attente selon les nœuds`
        : "Mises à jour inégales entre les nœuds";
    }
    case "node_stats_unavailable":
      // The cluster is fine; it is moxy's token that may not read the node
      // statistics (Sys.Audit missing on /nodes).
      return `Mesures CPU et mémoire indisponibles${on}`;
    default:
      return "Alerte";
  }
}

/**
 * The time column of the task journal: `12:00:02` for today, `11/09 04:26:34`
 * for any other day, `11/09/2025 04:26:34` for another year.
 *
 * The clock alone was enough for the sample data, which spans an evening. In
 * production, twenty-five to fifty tasks cover several days — nightly backups
 * see to that — and "04:26:34" from the day before yesterday looks exactly
 * like "04:26:34" from last night.
 *
 * The date is only written when it is needed. A column of dates where every
 * row is today reads worse than a column of clock times, and the journal is
 * mostly read about what just happened.
 */
export function formatTaskTime(iso: string, now: Date = new Date()): string {
  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) {
    return FALLBACK;
  }
  if (!(now instanceof Date) || Number.isNaN(now.getTime())) {
    return formatTime(iso);
  }

  const pad = (value: number) => value.toString().padStart(2, "0");
  const sameDay =
    date.getFullYear() === now.getFullYear() &&
    date.getMonth() === now.getMonth() &&
    date.getDate() === now.getDate();
  if (sameDay) {
    return formatTime(iso);
  }

  const day = `${pad(date.getDate())}/${pad(date.getMonth() + 1)}`;
  const stamp =
    date.getFullYear() === now.getFullYear()
      ? day
      : `${day}/${String(date.getFullYear())}`;
  return `${stamp} ${formatTime(iso)}`;
}

/**
 * Clock time of an ISO timestamp, as the task journal shows it: `12:00:02`.
 *
 * Formatted by hand rather than through Intl so the output is identical in the
 * browser and under Node, which keeps the tests deterministic.
 */
export function formatTime(iso: string): string {
  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) {
    return FALLBACK;
  }
  const pad = (value: number) => value.toString().padStart(2, "0");
  return `${pad(date.getHours())}:${pad(date.getMinutes())}:${pad(date.getSeconds())}`;
}

/**
 * The same clock time without its seconds: `12:00`, as the axis marks of
 * appendix A.1 write them.
 *
 * A tick every thirty minutes has no use for a second, and three labels
 * carrying one would be three times the width for no information.
 */
export function formatClock(iso: string): string {
  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) {
    return FALLBACK;
  }
  const pad = (value: number) => value.toString().padStart(2, "0");
  return `${pad(date.getHours())}:${pad(date.getMinutes())}`;
}

/**
 * French names for the PVE task types seen in a cluster journal.
 *
 * Anything absent falls back to the raw type, which is better than hiding a
 * task behind a generic label: an operator can search for the raw word.
 */
const TASK_TYPES: Record<string, string> = {
  vzdump: "Sauvegarde",
  qmstart: "Démarrage",
  qmstop: "Arrêt",
  qmshutdown: "Extinction",
  qmreboot: "Redémarrage",
  qmigrate: "Migration",
  qmclone: "Clonage",
  qmcreate: "Création",
  qmdestroy: "Suppression",
  qmsnapshot: "Instantané",
  vzstart: "Démarrage",
  vzstop: "Arrêt",
  vzshutdown: "Extinction",
  vzmigrate: "Migration",
  vzcreate: "Création",
  vzdestroy: "Suppression",
  aptupdate: "Mise à jour des paquets",
  srvstart: "Démarrage du service",
  srvstop: "Arrêt du service",
  srvreload: "Rechargement du service",
  srvrestart: "Redémarrage du service",
  imgcopy: "Copie d'image",
  imgdel: "Suppression d'image",
  download: "Téléchargement",
  hamigrate: "Migration HA",
  harelocate: "Relocalisation HA",
  auth_realm_sync: "Synchronisation d'annuaire",
  "auth-realm-sync": "Synchronisation d'annuaire",
  startall: "Démarrage groupé",
  stopall: "Arrêt groupé",
  migrateall: "Migration groupée",
  spiceproxy: "Console SPICE",
  vncproxy: "Console",
  termproxy: "Terminal",

  // The rest of task_desc_table, from pve-manager's www/manager6/Utils.js.
  // A type that is missing here falls back to its raw name, which is English
  // in a French interface: "qmresume · 103" in a column of sentences.
  qmresume: "Reprise",
  qmsuspend: "Suspension",
  qmpause: "Mise en pause",
  qmtemplate: "Conversion en modèle",
  qmrestore: "Restauration",
  qmsnapshotdelete: "Suppression d'instantané",
  qmdelsnapshot: "Suppression d'instantané",
  qmrollback: "Retour à un instantané",
  qmmove: "Déplacement de disque",
  qmconfig: "Modification de configuration",
  qmreset: "Réinitialisation",
  vzrestore: "Restauration",
  vzsnapshot: "Instantané",
  vzdelsnapshot: "Suppression d'instantané",
  vzrollback: "Retour à un instantané",
  vzclone: "Clonage",
  vzreboot: "Redémarrage",
  vzsuspend: "Suspension",
  vzresume: "Reprise",
  vztemplate: "Conversion en modèle",
  vzmount: "Montage",
  vzumount: "Démontage",

  // HA. These are CRM decisions rather than operator commands, which is worth
  // reading as such in a journal.
  hastart: "Démarrage HA",
  hastop: "Arrêt HA",
  hashutdown: "Extinction HA",

  // Storage and volumes.
  resize: "Redimensionnement",
  move_volume: "Déplacement de volume",
  move_disk: "Déplacement de disque",
  imgdelete: "Suppression d'image",
  unknownimgdel: "Suppression d'image orpheline",
  wipedisk: "Effacement de disque",

  // Certificates.
  acmenewcert: "Nouveau certificat ACME",
  acmerenew: "Renouvellement ACME",
  acmerevoke: "Révocation ACME",

  // Ceph.
  cephcreateosd: "Création d'OSD Ceph",
  cephdestroyosd: "Suppression d'OSD Ceph",
  cephcreatepool: "Création de pool Ceph",
  cephdestroypool: "Suppression de pool Ceph",
  cephcreatemon: "Création de moniteur Ceph",
  cephdestroymon: "Suppression de moniteur Ceph",
  cephcreatemds: "Création de MDS Ceph",
  cephdestroymds: "Suppression de MDS Ceph",
  cephfscreate: "Création de CephFS",

  // Cluster and node.
  clusterjoin: "Adhésion au cluster",
  clustercreate: "Création du cluster",
  reboot: "Redémarrage du nœud",
  shutdown: "Extinction du nœud",
  pull_file: "Copie de fichier",
  push_file: "Copie de fichier",
  dircreate: "Création de répertoire",
  diskinit: "Initialisation de disque",
  lvmcreate: "Création de volume LVM",
  lvmthincreate: "Création de pool LVM-thin",
  zfscreate: "Création de pool ZFS",

  unknown: "Tâche",
};

/**
 * One readable line for a task: what happened, and to what.
 *
 * The id carries the subject — a VMID for a guest operation, a realm name for
 * a directory sync — so it is appended when it adds anything.
 */
export function formatTaskLabel(task: {
  type: string;
  id: string;
  node: string;
}): string {
  const action = TASK_TYPES[task.type] ?? task.type;
  const subject = task.id.trim();
  if (subject === "" || subject === task.node) {
    return `${action} · ${task.node}`;
  }
  return `${action} · ${subject}`;
}

const TASK_OUTCOME_LABELS: Record<TaskOutcome, string> = {
  running: "En cours",
  ok: "OK",
  warnings: "Avertissements",
  failed: "Échec",
};

/**
 * The state tag of a task: `OK`, `Échec`, `En cours`, and `Avertissements (2)`
 * for a job that ran to completion and reported something worth a look.
 *
 * The count is appended when the backend could read one — PVE writes it into
 * the status string, and two warnings on a backup of ninety guests is not the
 * same news as thirty. Without a count the word stands alone rather than
 * showing a parenthesis around nothing.
 */
export function formatTaskOutcome(
  outcome: TaskOutcome,
  warnings?: number | null,
): string {
  const label = TASK_OUTCOME_LABELS[outcome];
  if (label === undefined) return "Alerte";
  if (outcome !== "warnings" || !isUsableNumber(warnings) || warnings <= 0) {
    return label;
  }
  return `${label} (${String(Math.floor(warnings))})`;
}
