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
 * TWO KINDS OF FUNCTION LIVE HERE, and the split is the whole shape of the
 * file:
 *
 *   - Those whose output does not depend on the language are plain module
 *     exports: a MAC address, a volume id, `VLAN 120`, a clock in 24 hours,
 *     `VM 103`. They are imported and called as they always were.
 *   - Everything else hangs off `createFormat(locale)`, because it carries
 *     either words or number typography. Components get one through
 *     `useFormat()` and destructure it; nothing reaches for a locale itself.
 *
 * The words come from the catalogue in `i18n/messages.ts`; the typography does
 * not. A decimal separator and the narrow no-break space before a French `%`
 * are punctuation, decided per locale in the table below, and a translator has
 * no business editing them.
 *
 * Typographic conventions, per language:
 *   - French: decimal comma (`1,2 TiB`), U+202F NARROW NO-BREAK SPACE before
 *     `%` and as the thousands separator (`31 %`, `1 024`). U+202F is the
 *     modern French standard for both and is what current ICU emits for
 *     `fr-FR`; a plain space is not used because a line break between a number
 *     and its `%`, or in the middle of `1 024`, is a rendering bug.
 *   - English: decimal point (`1.2 TiB`), comma for thousands (`1,024`), and no
 *     space at all before the `%` (`31%`).
 *   - In both: a plain space between a number and any other unit (`61 GiB`,
 *     `47 min`), matching the mockups.
 *   - In both: no function throws. An aberrant input (NaN, Infinity, negative)
 *     renders the em dash fallback, which reads as "unknown" in the UI.
 */

import type {
  Alert,
  ApiError,
  Allocation,
  ClusterStatus,
  DiskUsage,
  GuestKind,
  GuestStatus,
  MaintenanceErrorKind,
  MaintenanceResult,
  NodeStatus,
  Quorum,
  TaskOutcome,
  Timeframe,
  Usage,
} from "@/api/types";
import {
  isMessageKey,
  translator,
  type MessageKey,
  type Translator,
} from "@/i18n/messages";
import type { Locale } from "@/lib/lang";

/** U+202F, narrow no-break space: French uses it before `%` and for thousands. */
export const NNBSP = " ";

/** Rendered for any value we cannot express: NaN, Infinity, negative sizes. */
export const FALLBACK = "—";

/**
 * The IEC prefixes, which are not translated — only the base unit is, `o` in
 * French and `B` in English, and it comes from the catalogue.
 */
const BYTE_PREFIXES = ["", "KiB", "MiB", "GiB", "TiB", "PiB"] as const;

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
 * How a language punctuates a figure.
 *
 * Not in the message catalogue on purpose: these are not words. A translator
 * adding a language edits `i18n/messages.ts`; the separators and the space
 * before a percent sign are decided here, next to the code that applies them.
 */
interface Typography {
  /** Between the integer part and the decimals. */
  decimal: string;
  /** Between groups of three digits. */
  group: string;
  /** How a percentage is written around its figure. */
  percent: (figure: string) => string;
  /** How "less than this much" is written, for a ratio too small to render. */
  belowPercent: (figure: string) => string;
}

const TYPOGRAPHY: Record<Locale, Typography> = {
  fr: {
    decimal: ",",
    group: NNBSP,
    percent: (figure) => `${figure}${NNBSP}%`,
    belowPercent: (figure) => `<${NNBSP}${figure}${NNBSP}%`,
  },
  en: {
    decimal: ".",
    group: ",",
    percent: (figure) => `${figure}%`,
    belowPercent: (figure) => `< ${figure}%`,
  },
};

/*
 * ---------------------------------------------------------------------------
 * Locale-independent formatting.
 *
 * What follows carries neither a word nor a separator, so it is the same string
 * in every language and stays a plain export. Adding a language changes none of
 * it; adding a word to any of it means moving it into `createFormat` below.
 * ---------------------------------------------------------------------------
 */

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
 * The short designation of a guest: `VM 103`, `CT 105`.
 *
 * "CT" is PVE's own abbreviation for a container, which is what an operator
 * reads in the native interface and in `pct`. A breadcrumb calling an LXC
 * "VM 105" contradicts every other tool they use — in either language, which
 * is why this one does not go through the catalogue.
 */
export function formatGuestRef(kind: GuestKind, vmid: number): string {
  const prefix = kind === "lxc" ? "CT" : "VM";
  if (!isUsableNumber(vmid)) return prefix;
  return `${prefix} ${String(Math.trunc(vmid))}`;
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

/**
 * The VLAN of an interface, `VLAN 120`, and null when the link carries none.
 *
 * Null is UNTAGGED, which is a fact about the link and not a missing reading:
 * the caller leaves the cell empty rather than rendering the em dash it uses
 * for what nobody could measure.
 */
export function formatVlan(tag: number | null): string | null {
  if (tag === null) return null;
  return `VLAN ${String(tag)}`;
}

/**
 * A volume id with the storage it already sits next to taken off the front.
 *
 * PVE writes a volume as `storage:path`, and the storage of a row is that very
 * prefix — `parseConfigDisk` obtains one by cutting the other. Printing both
 * spells the storage twice on every line and pushes the part that actually
 * distinguishes two volumes, `vm-100-disk-0`, into a wrap.
 *
 * The prefix comes off only when it IS the row's storage. A volume that starts
 * with something else is an anomaly worth seeing, not one to trim into
 * agreement; and a device passed straight through to the guest has no storage
 * at all and an absolute path that may itself contain a colon, so it is
 * returned untouched — the same colon trap the backend guards against.
 *
 * The full id stays in the payload: it is what PVE stores, and what `qm
 * config` or a migration command expects.
 */
export function formatVolumeName(volume: string, storage: string | null): string {
  if (storage === null || storage === "") return volume;
  const prefix = `${storage}:`;
  return volume.startsWith(prefix) ? volume.slice(prefix.length) : volume;
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

/**
 * Clock time of an ISO timestamp, as the task journal shows it: `12:00:02`.
 *
 * Formatted by hand rather than through Intl so the output is identical in the
 * browser and under Node, which keeps the tests deterministic.
 *
 * Twenty-four hours in both languages. `en` is not `en-US`: opening the door to
 * a 12-hour clock would bring the whole question of regional formats — and of
 * which region an `en` browser is in — for a journal whose rows an operator
 * compares against `journalctl`.
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
 *
 * Day before month in both languages, as `formatDateTime` writes it too. The
 * alternative is not "English order" but American order, and serving `09/11` to
 * a British browser to please an American one trades one ambiguity for another.
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
 * An axis mark, written at the precision its window deserves.
 *
 * A clock names an instant inside an hour and inside a day, and says nothing
 * beyond: three marks reading `14:00 · 02:00 · 14:00` under a thirty-day
 * window name three instants an operator cannot place. So past the day the
 * mark carries the date, and past the month the month — never both at once,
 * three labels under a chart three hundred units wide having no room for a
 * date and a time.
 */
export function formatAxisTime(iso: string, timeframe: Timeframe): string {
  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) {
    return FALLBACK;
  }
  const pad = (value: number) => value.toString().padStart(2, "0");
  switch (timeframe) {
    case "hour":
    case "day":
      return formatClock(iso);
    case "week":
    case "month":
      return `${pad(date.getDate())}/${pad(date.getMonth() + 1)}`;
    case "year":
      return `${pad(date.getMonth() + 1)}/${date.getFullYear().toString()}`;
  }
}

/*
 * ---------------------------------------------------------------------------
 * Locale-dependent formatting.
 * ---------------------------------------------------------------------------
 */

/**
 * The nouns `plural` can count.
 *
 * A closed set rather than two free strings: the words then live in the
 * catalogue, where the English translation of each is checked by the compiler,
 * instead of being written at every call site in the language of whoever wrote
 * it. `plural.<noun>.one` and `plural.<noun>.other` must both exist.
 */
export type PluralNoun =
  | "cluster"
  | "node"
  | "vm"
  | "alert"
  | "guest"
  | "template"
  | "stopped"
  | "package"
  | "disk"
  | "net"
  | "detachedVolume"
  | "result";

/** The two halves a metric card shows for a used/total pair. */
export interface UsageParts {
  /** The headline figure: the fill percentage, `83 %`. */
  value: string;
  /** The quieter half, separator included: `· 212 / 256 GiB`. */
  detail: string | undefined;
}

/** Everything whose output depends on the display language. */
export interface Format {
  formatBytes: (bytes: number) => string;
  formatUsage: (usage: Usage | DiskUsage | null | undefined) => string;
  formatUsageParts: (usage: Usage | DiskUsage | null | undefined) => UsageParts;
  formatUsageLine: (usage: Usage | DiskUsage | null | undefined) => string;
  formatRatio: (ratio: number | null | undefined, digits?: number) => string;
  formatCores: (cores: number | null | undefined) => string;
  formatVcpus: (cores: number | null | undefined) => string;
  formatLoadAverage: (
    load: readonly [number, number, number] | null | undefined,
  ) => string;
  formatUptime: (seconds: number | null) => string;
  formatRelativeTime: (date: Date, now?: Date) => string;
  formatNodeStatus: (status: NodeStatus) => string;
  formatGuestStatus: (status: GuestStatus) => string;
  formatStayingReason: (reason: string) => string;
  formatGuestKind: (kind: GuestKind) => string;
  formatQuorum: (quorum: Quorum | null | undefined) => string;
  formatDateTime: (date: Date | null | undefined) => string | null;
  formatInteger: (value: number) => string;
  plural: (count: number, noun: PluralNoun) => string;
  formatPendingUpdates: (pending: number | null) => string | null;
  formatPackageCount: (count: number) => string;
  formatDiskCount: (count: number) => string;
  formatNetCount: (count: number) => string;
  formatMatchCount: (count: number) => string;
  formatAllocationQualifier: (allocation: Allocation | null) => string | null;
  formatDetachedVolumes: (allocation: Allocation | null) => string | null;
  formatHaState: (state: string | null | undefined) => string | null;
  formatErrorKind: (error: ApiError | null | undefined) => string | null;
  formatMaintenanceError: (kind: string | null | undefined) => string;
  formatMaintenanceOutcome: (result: MaintenanceResult) => string;
  formatClusterStatus: (status: ClusterStatus) => string;
  formatAlert: (alert: Alert) => string;
  formatTaskLabel: (task: { type: string; id: string; node: string }) => string;
  formatTaskOutcome: (outcome: TaskOutcome, warnings?: number | null) => string;
  formatTimeframe: (timeframe: Timeframe) => string;
  formatTimeframeShort: (timeframe: Timeframe) => string;
}

/**
 * One message per refusal the maintenance route can answer with.
 *
 * A table rather than a `maintenance.error.${kind}` lookup: the keys of this
 * catalogue are camel case throughout — `plan.blocker.noTarget` already
 * translates the blocker `no_target` — and a template literal would force the
 * backend's snake case into it for one group of thirteen. Written out, it is
 * also exhaustive by type: a kind added to `MaintenanceErrorKind` without a
 * sentence fails `typecheck`.
 */
const MAINTENANCE_ERRORS: Record<MaintenanceErrorKind, MessageKey> = {
  maintenance_forbidden: "maintenance.error.forbidden",
  no_quorum: "maintenance.error.noQuorum",
  no_ha_manager: "maintenance.error.noHaManager",
  no_other_node: "maintenance.error.noOtherNode",
  already_running: "maintenance.error.alreadyRunning",
  keysource_unavailable: "maintenance.error.keysourceUnavailable",
  keysource_denied: "maintenance.error.keysourceDenied",
  ssh_unreachable: "maintenance.error.sshUnreachable",
  ssh_host_key_mismatch: "maintenance.error.sshHostKeyMismatch",
  ssh_auth_failed: "maintenance.error.sshAuthFailed",
  ssh_timeout: "maintenance.error.sshTimeout",
  command_refused: "maintenance.error.commandRefused",
  command_failed: "maintenance.error.commandFailed",
};

/**
 * Binds every language-dependent formatter to one locale.
 *
 * Called once per locale by the provider in `i18n/locale.tsx`, never inside a
 * render: the object is stable for as long as the language is, so a component
 * may destructure it and pass its functions wherever a callback is expected.
 */
export function createFormat(locale: Locale): Format {
  const t: Translator = translator(locale);
  const typography = TYPOGRAPHY[locale];

  /**
   * Renders a finite number with at most `digits` decimals.
   * Trailing zeros are dropped: a decimal is shown only when it carries
   * information, so 61.0 renders `61` and 1.2 renders `1,2` (or `1.2`).
   */
  function formatNumber(value: number, digits: number): string {
    const negative = value < 0;
    const fixed = Math.abs(roundTo(value, digits)).toFixed(Math.max(0, digits));
    let [integer = "0", fraction = ""] = fixed.split(".");
    fraction = fraction.replace(/0+$/, "");
    const grouped = integer.replace(/\B(?=(\d{3})+(?!\d))/g, typography.group);
    const body = fraction ? `${grouped}${typography.decimal}${fraction}` : grouped;
    return negative ? `-${body}` : body;
  }

  /** The base unit of a byte count, which is the only translated one. */
  const baseUnit = () => t("unit.bytes");

  function unitAt(exponent: number): string {
    return BYTE_PREFIXES[exponent] === undefined || exponent === 0
      ? baseUnit()
      : BYTE_PREFIXES[exponent];
  }

  interface ScaledBytes {
    /** Value expressed in `unit`. */
    value: number;
    unit: string;
    /** Decimals to display for this magnitude. */
    digits: number;
    /** Index in `BYTE_PREFIXES`, so a caller can express another value here. */
    exponent: number;
  }

  /**
   * Expresses a byte count in an imposed unit, with the precision rule below:
   * no decimal at or above 100, one decimal under it, raw bytes for the base.
   */
  function scaleAt(bytes: number, exponent: number): ScaledBytes {
    const value = bytes / 1024 ** exponent;
    return {
      value,
      unit: unitAt(exponent),
      digits: exponent === 0 || value >= 100 ? 0 : 1,
      exponent,
    };
  }

  /**
   * Picks the binary unit and the precision for a byte count.
   *
   * Precision rule, taken from the mockups (`1,2 TiB`, `61 GiB`, `418 GiB`,
   * `28 GiB`): at or above 100 the decimal is noise, so none is shown; below
   * 100 one decimal is computed and kept only when it is significant. A value
   * that rounds up to 1024 is promoted to the next unit so that 1023.7 GiB
   * renders `1 TiB` and never `1 024 GiB`.
   */
  function scaleBytes(bytes: number): ScaledBytes {
    let exponent = 0;
    let value = bytes;
    const last = BYTE_PREFIXES.length - 1;
    while (value >= 1024 && exponent < last) {
      value /= 1024;
      exponent += 1;
    }
    // Bytes are counted, never fractional; larger units get one decimal below
    // 100.
    let digits = exponent === 0 || value >= 100 ? 0 : 1;
    if (roundTo(value, digits) >= 1024 && exponent < last) {
      value /= 1024;
      exponent += 1;
      digits = value >= 100 ? 0 : 1;
    }
    return { value, unit: unitAt(exponent), digits, exponent };
  }

  /**
   * Renders a byte count with its binary unit: `1,2 TiB`, `61 GiB`, `418 GiB`.
   * Negative, NaN and Infinity render the fallback; 0 renders `0 o` / `0 B`.
   */
  function formatBytes(bytes: number): string {
    if (!isUsableNumber(bytes) || bytes < 0) return FALLBACK;
    const scaled = scaleBytes(bytes);
    return `${formatNumber(scaled.value, scaled.digits)} ${scaled.unit}`;
  }

  /**
   * Renders a used/total pair with the unit written once: `212 / 256 GiB`,
   * `418 / 1 024 GiB`.
   *
   * Both values share one unit so that the fill level can be judged at a
   * glance. `418 GiB / 1 TiB` forces a mental conversion before the reader
   * knows whether the node is half full — exactly the friction this UI exists
   * to remove — so the total is expressed in the used value's magnitude when
   * they differ, and `1 024 GiB` is preferred to `1 TiB`. This is what the four
   * cluster cards of appendix A.4 show.
   *
   * The shared unit is the one of the total, stepped down by **one** unit when
   * the used value would otherwise read below 1 in it. One step is the whole
   * rule: it covers every pair within a factor of 1024 of each other, which is
   * every realistic used/total pair.
   *
   * Exception — when one step is not enough, the two values are more than a
   * unit apart and no shared unit can hold both: the used value would round to
   * 0 (`0 / 8 TiB`, a lie) or the total would need seven digits
   * (`980 / 8 388 608 MiB`, unreadable). Separate units are then the honest
   * rendering: `980 MiB / 8 TiB`.
   *
   * A used value of exactly 0 keeps the shared unit — `0 / 8 TiB` is true and
   * reads fine — and an aberrant or empty total renders the fallback.
   */
  function formatUsage(usage: Usage | DiskUsage | null | undefined): string {
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
   * Splits a used/total pair the way the metric cards display it: the fill
   * percentage as the headline figure, the pair itself as the quiet detail —
   * `83 %` then `· 212 / 256 GiB`.
   *
   * How full a node is is the question asked first, and it was the only one of
   * the four metrics left to the reader to divide in their head: the CPU card
   * next door already reads `31 % · 32 c`. The pair stays because a percentage
   * alone loses the volume behind it — 83 % of a node does not say whether 4 or
   * 400 GiB are left.
   *
   * The percentage is the ratio the API serves, never a local `used / total`:
   * shared capacity is counted per backend upstream (ADR 0002), so a recomputed
   * ratio would quietly disagree with the bar drawn beside it.
   *
   * Either half unknown collapses to the other one alone: `— · 212 / 256 GiB`
   * and `83 % · —` both promise a figure their other half cannot back, and
   * `— · —` says one ignorance twice.
   */
  function formatUsageParts(usage: Usage | DiskUsage | null | undefined): UsageParts {
    const pair = formatUsage(usage);
    const percent = formatRatio(usage?.ratio ?? null);
    if (percent === FALLBACK) return { value: pair, detail: undefined };
    if (pair === FALLBACK) return { value: percent, detail: undefined };
    return { value: percent, detail: `· ${pair}` };
  }

  /**
   * The same pair as one string, for the rows that have no detail slot to put
   * the quiet half in: `83 % · 212 / 256 GiB`.
   */
  function formatUsageLine(usage: Usage | DiskUsage | null | undefined): string {
    const { value, detail } = formatUsageParts(usage);
    return detail === undefined ? value : `${value} ${detail}`;
  }

  /**
   * Renders a ratio in [0,1] as a percentage: 0.31 → `31 %`, 0.828 → `83 %`,
   * 0.0025 → `0,25 %`. In English the space before the sign is dropped: `31%`.
   *
   * Without an explicit `digits`, precision adapts to the magnitude the way the
   * mockups do — whole percents above 10 (`31 %`), one decimal in between
   * (`3,1 %`), two below one percent (`0,25 %`) — and trailing zeros are
   * dropped, so 0.04 renders `4 %` and not `4,0 %`.
   *
   * A non-zero ratio never renders `0 %`, which would read as "nothing": below
   * the smallest representable value it renders `< 0,1 %` (or `< 1 %` when the
   * caller pinned `digits` to 0). Exactly 0 renders `0 %`, and an unknown ratio
   * (`null`) renders the fallback: the backend sends null, never a fake zero.
   */
  function formatRatio(ratio: number | null | undefined, digits?: number): string {
    if (ratio === null || ratio === undefined || !isUsableNumber(ratio) || ratio < 0) {
      return FALLBACK;
    }
    if (digits !== undefined && (!isUsableNumber(digits) || digits < 0)) {
      return FALLBACK;
    }

    const percent = ratio * 100;
    if (percent === 0) return typography.percent("0");

    if (digits === undefined) {
      // Below 0,1 % the UI says so explicitly rather than inventing decimals.
      if (percent < 0.1) return typography.belowPercent(formatNumber(0.1, 1));
      const adaptive = percent >= 10 ? 0 : percent >= 1 ? 1 : 2;
      return typography.percent(formatNumber(percent, adaptive));
    }

    const pinned = Math.min(Math.trunc(digits), 6);
    if (roundTo(percent, pinned) === 0) {
      return typography.belowPercent(formatNumber(10 ** -pinned, pinned));
    }
    return typography.percent(formatNumber(percent, pinned));
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
  function formatCores(cores: number | null | undefined): string {
    if (cores === null || cores === undefined) return FALLBACK;
    if (!isUsableNumber(cores) || cores <= 0) return FALLBACK;
    return `${formatNumber(cores, 0)} ${t("unit.cores")}`;
  }

  /**
   * The vCPU count of a guest: `4 vCPU`.
   *
   * "vCPU" is invariable, as PVE writes it. A zero is not a guest with no
   * processor — PVE always assigns at least one — it is a figure that did not
   * arrive, so it renders the dash.
   */
  function formatVcpus(cores: number | null | undefined): string {
    if (cores === null || cores === undefined) return FALLBACK;
    if (!isUsableNumber(cores) || cores <= 0) return FALLBACK;
    return `${formatNumber(cores, 0)} ${t("unit.vcpus")}`;
  }

  /**
   * The three load average figures, in the order the kernel reports them:
   * `0,84 · 0,91 · 0,88`.
   *
   * It goes through formatNumber like every other figure of the UI, so that the
   * decimal separator, the thousands separator and the rounding are decided in
   * one place. A toFixed written in a screen would drift the day any of the
   * three changes, silently and only there.
   *
   * A genuine `0,00 0,00 0,00` IS shown: a quiet node really reports it, and
   * turning that into "unknown" would be the symmetrical lie. Only an absent or
   * unusable reading renders the dash.
   */
  function formatLoadAverage(
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
  function formatUptime(seconds: number | null): string {
    // null is "no uptime to report": an offline node, a stopped guest, a node
    // the token may not audit. The em dash says so; a "0 s" would claim the
    // machine came up this very second.
    if (!isUsableNumber(seconds) || seconds < 0) return FALLBACK;

    const total = Math.floor(seconds);
    const parts = [
      { value: Math.floor(total / 86400), unit: t("unit.day") },
      { value: Math.floor(total / 3600) % 24, unit: t("unit.hour") },
      { value: Math.floor(total / 60) % 60, unit: t("unit.minute") },
      { value: total % 60, unit: t("unit.second") },
    ];

    const zero = `0 ${t("unit.second")}`;
    const first = parts.findIndex((part) => part.value > 0);
    if (first === -1) return zero;

    const head = parts[first];
    if (!head) return zero;
    const tail = parts[first + 1];
    const text = `${head.value} ${head.unit}`;
    return tail && tail.value > 0 ? `${text} ${tail.value} ${tail.unit}` : text;
  }

  /**
   * Renders how long ago `date` happened, used for data freshness:
   * `à l'instant`, `il y a 12 s`, `il y a 3 min` — `just now`, `12 s ago`.
   *
   * A single unit is enough here — the reader wants staleness, not a duration.
   * Anything under five seconds, and any date in the future (clock skew between
   * the browser and the hypervisors is routine), reads as "just now".
   */
  function formatRelativeTime(date: Date, now: Date = new Date()): string {
    if (!(date instanceof Date) || Number.isNaN(date.getTime())) return FALLBACK;
    if (!(now instanceof Date) || Number.isNaN(now.getTime())) return FALLBACK;

    const ago = (value: number, unit: MessageKey) =>
      t("time.ago", { duration: `${String(value)} ${t(unit)}` });

    const seconds = Math.floor((now.getTime() - date.getTime()) / 1000);
    if (seconds < 5) return t("time.justNow");
    if (seconds < 60) return ago(seconds, "unit.second");
    const minutes = Math.floor(seconds / 60);
    if (minutes < 60) return ago(minutes, "unit.minute");
    const hours = Math.floor(minutes / 60);
    if (hours < 24) return ago(hours, "unit.hour");
    return ago(Math.floor(hours / 24), "unit.day");
  }

  /** Sentence-case label of a node status. */
  function formatNodeStatus(status: NodeStatus): string {
    // The union says this cannot miss; the payload it comes from says nothing
    // of the sort, so an unknown status reads as unknown rather than blank.
    const key: string = `status.node.${status}`;
    return isMessageKey(key) ? t(key) : t("status.node.unknown");
  }

  /**
   * State of a guest, in the ONE word this interface uses for it.
   *
   * "template", "Template" and "Modèle" all named the same state in different
   * places, which reads as three states. Sentence case, like every other label.
   */
  function formatGuestStatus(status: GuestStatus): string {
    const key: string = `status.guest.${status}`;
    return isMessageKey(key) ? t(key) : t("status.node.unknown");
  }

  /**
   * Why a guest stays where it is during a drain.
   *
   * The backend emits a stable key, not a sentence — `template` is the only one
   * the plan produces today — and the translation belongs here rather than in a
   * ternary inside the dialog, which is where it used to live: a second key
   * would otherwise reach the screen in English, and "template" would read as a
   * different state from the "Modèle" the rest of the interface says.
   */
  function formatStayingReason(reason: string): string {
    return reason === "template" ? t("status.guest.template") : reason;
  }

  /**
   * What kind of guest this is, spelled out: a container is not a virtual
   * machine, and calling it one is how a breadcrumb ends up reading "VM 105"
   * about an LXC.
   */
  function formatGuestKind(kind: GuestKind): string {
    const key: string = `guestKind.${kind}`;
    return isMessageKey(key) ? t(key) : t("guestKind.unknown");
  }

  /**
   * The quorum line of a node: `OK · 3/3 votes`, `Perdu · 1/3 votes`, and
   * `Nœud seul` for a machine that belongs to no cluster.
   *
   * A standalone node has no quorum at all — inventing a one-node vote would
   * put a healthy machine in a state it is not in — which is why null is a
   * sentence of its own rather than the dash.
   */
  function formatQuorum(quorum: Quorum | null | undefined): string {
    if (quorum === null || quorum === undefined) return t("quorum.standalone");
    const { quorate, online, nodes } = quorum;
    if (!isUsableNumber(online) || !isUsableNumber(nodes)) return FALLBACK;
    return t("quorum.votes", {
      verdict: quorate ? t("quorum.ok") : t("quorum.lost"),
      online: formatNumber(online, 0),
      nodes: formatNumber(nodes, 0),
    });
  }

  /**
   * `12/09/2026 à 14:32`, the stamp of the staleness banner.
   *
   * Built by hand rather than through Intl for the reason stated at the top of
   * this file: ICU output drifts between Node builds, and these strings are
   * asserted character by character. Null for a date that cannot be read, so
   * the caller writes a sentence with no timestamp rather than "Invalid Date".
   */
  function formatDateTime(date: Date | null | undefined): string | null {
    if (!(date instanceof Date) || Number.isNaN(date.getTime())) {
      return null;
    }
    const pad = (value: number) => String(value).padStart(2, "0");
    const day = `${pad(date.getDate())}/${pad(date.getMonth() + 1)}/${String(date.getFullYear())}`;
    return t("time.dateAt", {
      date: day,
      time: `${pad(date.getHours())}:${pad(date.getMinutes())}`,
    });
  }

  /** A count, grouped the way every other figure of the UI is: `1 024`. */
  function formatInteger(value: number): string {
    if (!isUsableNumber(value)) return FALLBACK;
    return formatNumber(Math.trunc(value), 0);
  }

  /**
   * `1 nœud` / `3 nœuds`, `1 node` / `3 nodes`.
   *
   * One rule serves both languages: the singular at exactly one, the plural
   * everywhere else — a zero takes the plural in each. Only the words differ,
   * and they come from the catalogue rather than from the call site.
   *
   * It lives here rather than in the three screens that each had their own
   * copy: the rule is typographic, and typography is decided once.
   */
  function plural(count: number, noun: PluralNoun): string {
    if (!isUsableNumber(count)) return FALLBACK;
    const n = Math.trunc(count);
    return `${formatNumber(n, 0)} ${t(n === 1 ? `plural.${noun}.one` : `plural.${noun}.other`)}`;
  }

  /**
   * The value of the "Mises à jour" row of a node: `À jour` when nothing is
   * pending, `12 en attente` otherwise, and null when the question could not be
   * asked — the caller renders that as the dash, never as a reassuring zero.
   */
  function formatPendingUpdates(pending: number | null): string | null {
    if (pending === null) return null;
    // The noun is invariable in both languages here: the count carries it.
    return pending === 0
      ? t("nodeUpdates.upToDate")
      : t("nodeUpdates.pending", { count: pending });
  }

  /**
   * `1 résultat` / `4 résultats` / `Aucun résultat`, for the live region of the
   * tree.
   *
   * It moved out of lib/search.ts, which matches strings and has no business
   * writing one: a sentence shown to a user is built here, like every other.
   */
  function formatMatchCount(count: number): string {
    if (count === 0) return t("search.noResult");
    return plural(count, "result");
  }

  /**
   * The qualifier next to a guest's allocated volumetry.
   *
   * `· alloué` when every attached volume declares a size, `· au moins` when
   * one does not: the total is then a floor, and saying so is the whole point —
   * a bare number would claim a precision the configuration does not carry.
   * Null when the configuration could not be read at all, which the caller
   * renders as the em dash rather than as a total of zero.
   */
  function formatAllocationQualifier(allocation: Allocation | null): string | null {
    if (allocation === null) return null;
    return allocation.partial ? t("allocation.atLeast") : t("allocation.allocated");
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
  function formatDetachedVolumes(allocation: Allocation | null): string | null {
    if (allocation === null || allocation.detached === 0) return null;
    const label = plural(allocation.detached, "detachedVolume");
    // PVE records no size for an unused volume, so this is usually unknown.
    return allocation.detachedBytes === 0
      ? label
      : `${label} (${formatBytes(allocation.detachedBytes)})`;
  }

  /**
   * The CRM's vocabulary for an HA resource or an HA node.
   *
   * The backend serves the manager's own word — `started`, `fence`, `migrate` —
   * because that is what an operator needs to read during an incident. Anything
   * the catalogue does not cover is shown as it came: a state invented by a
   * newer PVE is still more useful raw than hidden behind "unknown".
   */
  function formatHaState(state: string | null | undefined): string | null {
    if (state === null || state === undefined || state.trim() === "") return null;
    const key = `ha.${state}`;
    return isMessageKey(key) ? t(key) : state;
  }

  /**
   * Why a cluster could not be read.
   *
   * The backend classifies every failure and says so in `kind`, then leaves the
   * wording here — that is the whole contract. Leaving it untranslated meant
   * the card said "Lecture ancienne · il y a 12 min" for a revoked token, which
   * sends an operator looking at the network for something that is a two-minute
   * fix.
   */
  function formatErrorKind(error: ApiError | null | undefined): string | null {
    if (!error || typeof error.kind !== "string") return null;
    switch (error.kind) {
      case "auth":
        // 401 and 403 are both "auth" upstream and mean opposite errands: one
        // is a token that is no longer valid, the other a token missing a
        // privilege.
        if (error.status === 401) return t("errorKind.tokenRefused");
        if (error.status === 403) return t("errorKind.insufficientRights");
        return t("errorKind.authRefused");
      case "tls":
        return t("errorKind.tls");
      case "timeout":
        return t("errorKind.timeout");
      case "network":
        return t("errorKind.network");
      case "protocol":
        return t("errorKind.protocol");
      default:
        return error.kind;
    }
  }

  /**
   * Why a maintenance request was refused, in the display language.
   *
   * Same contract as `formatErrorKind` one function up: the backend classifies
   * and answers in English with a stable `kind`, the sentence is built here.
   * The difference is that this vocabulary is CLOSED — `internal/maintenance`
   * owns every value — so an unrecognised one is not shown raw: it would be a
   * word from a backend this build does not know, and "keysource_denied" on
   * screen explains nothing. It falls back to a sentence that at least says
   * the request did not go through.
   */
  function formatMaintenanceError(kind: string | null | undefined): string {
    if (typeof kind !== "string" || !Object.hasOwn(MAINTENANCE_ERRORS, kind)) {
      return t("maintenance.error.unknown");
    }
    return t(MAINTENANCE_ERRORS[kind as MaintenanceErrorKind]);
  }

  /**
   * What a successful maintenance request means, which is NEVER "the node is
   * drained".
   *
   * The CRM command writes an intention into the cluster filesystem; the drain
   * follows on its own, and the polling is what reports it. Saying otherwise
   * would have an operator switch a machine off while its guests are still
   * migrating.
   *
   * `alreadyInState` is not a failure and does not read as one: nothing ran
   * because there was nothing to run.
   */
  function formatMaintenanceOutcome(result: MaintenanceResult): string {
    if (result.alreadyInState) {
      return result.action === "enable"
        ? t("maintenance.alreadyEnable")
        : t("maintenance.alreadyDisable");
    }
    return result.action === "enable"
      ? t("maintenance.acceptedEnable", { node: result.node })
      : t("maintenance.acceptedDisable", { node: result.node });
  }

  /** Sentence-case label of a cluster status. */
  function formatClusterStatus(status: ClusterStatus): string {
    const key: string = `status.cluster.${status}`;
    return isMessageKey(key) ? t(key) : t("status.node.unknown");
  }

  /** `2 nœuds` / `1 nœud`, or an empty string when the count is unknown. */
  function nodeCount(nodes: string[] | undefined): string {
    if (!Array.isArray(nodes) || nodes.length === 0) return "";
    return plural(nodes.length, "node");
  }

  /**
   * Joins a few items into an enumeration: `9.2.9 et 9.2.12`,
   * `9.2.9, 9.2.11 et 9.2.12`.
   *
   * `Intl.ListFormat` would do it, and is deliberately not used — for the
   * reason stated at the top of this file, and for one of its own: the
   * conjunction has to be the one of the language ON SCREEN, not the one of
   * the viewer's system locale, which is what ICU would give. It comes from
   * the catalogue like every other word.
   */
  function formatList(items: string[]): string {
    const last = items[items.length - 1];
    if (last === undefined) return "";
    if (items.length === 1) return last;
    return `${items.slice(0, -1).join(", ")} ${t("list.and")} ${last}`;
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
  function formatAlert(alert: Alert): string {
    if (!alert || typeof alert.kind !== "string") return t("alert.generic");
    const count = nodeCount(alert.nodes);
    const on = count ? t("alert.on", { count }) : "";

    switch (alert.kind) {
      case "quorum_lost":
        return t("alert.quorumLost");
      case "unreachable":
        return t("alert.unreachable");
      case "node_offline":
        return count ? t("alert.nodeOffline", { count }) : t("alert.nodeOfflineOne");
      case "node_unknown":
        // Not "offline": the cluster never said this node is down, it simply
        // never mentioned it. A node that has just joined reads like this for a
        // few seconds, and a stale resource row of a departed node for as long
        // as PVE keeps it.
        return count ? t("alert.nodeUnknown", { count }) : t("alert.nodeUnknownOne");
      case "memory_high": {
        const ratio =
          alert.ratio !== undefined && isUsableNumber(alert.ratio) && alert.ratio >= 0
            ? formatRatio(alert.ratio)
            : "";
        if (!ratio) return t("alert.memoryHigh", { on });
        // With nodes named, the ratio is the highest of theirs, not the cluster
        // average -- "(max.)" says which figure this is, so that a single node
        // at 92 % in a cluster at 55 % reads as the node it is about.
        return count
          ? t("alert.memoryAtMax", { ratio, on })
          : t("alert.memoryAt", { ratio });
      }
      case "updates_available":
        return t("alert.updatesAvailable", {
          version: alert.version ? ` ${alert.version}` : "",
          on,
        });
      case "updates_uneven": {
        // No "on N nodes" suffix here: the alert is about the spread between
        // the nodes, not about a set of them. It degrades to the bare sentence
        // when the bounds are missing, like the other kinds carrying optional
        // fields.
        const { pendingMin: min, pendingMax: max } = alert;
        const bounded =
          min !== undefined &&
          max !== undefined &&
          isUsableNumber(min) &&
          isUsableNumber(max) &&
          min >= 0 &&
          max > min;
        return bounded
          ? t("alert.updatesUnevenBounded", { min, max })
          : t("alert.updatesUneven");
      }
      case "versions_uneven": {
        // No "on N nodes" suffix, same as updates_uneven: the alert is about
        // what separates the nodes, not about a set of them -- and the version
        // column of the card already says which node runs which.
        const versions = (alert.versions ?? []).filter(
          (v): v is string => typeof v === "string" && v !== "",
        );
        if (versions.length < 2) return t("alert.versionsUneven");
        // Named one by one while they fit on the banner's single line. Past
        // three, the count and the two ends say as much in less room -- and a
        // cluster spread over four releases is read for how bad it is, not for
        // the exact rungs.
        const spread =
          versions.length > 3
            ? t("alert.versionsUnevenSpread", {
                count: versions.length,
                first: versions[0] ?? "",
                last: versions[versions.length - 1] ?? "",
              })
            : formatList(versions);
        return t("alert.versionsUnevenList", { versions: spread });
      }
      case "node_stats_unavailable":
        // The cluster is fine; it is moxy's token that may not read the node
        // statistics (Sys.Audit missing on /nodes).
        return t("alert.statsUnavailable", { on });
      default:
        return t("alert.generic");
    }
  }

  /**
   * One readable line for a task: what happened, and to what.
   *
   * The id carries the subject — a VMID for a guest operation, a realm name for
   * a directory sync — so it is appended when it adds anything. A type the
   * catalogue does not name falls back to its raw word, which is better than
   * hiding a task behind a generic label: an operator can search for it.
   */
  function formatTaskLabel(task: { type: string; id: string; node: string }): string {
    const key = `task.type.${task.type}`;
    const action = isMessageKey(key) ? t(key) : task.type;
    const subject = task.id.trim();
    if (subject === "" || subject === task.node) {
      return `${action} · ${task.node}`;
    }
    return `${action} · ${subject}`;
  }

  /**
   * The state tag of a task: `OK`, `Échec`, `En cours`, and `Avertissements (2)`
   * for a job that ran to completion and reported something worth a look.
   *
   * The count is appended when the backend could read one — PVE writes it into
   * the status string, and two warnings on a backup of ninety guests is not the
   * same news as thirty. Without a count the word stands alone rather than
   * showing a parenthesis around nothing.
   */
  function formatTaskOutcome(outcome: TaskOutcome, warnings?: number | null): string {
    const key: string = `task.outcome.${outcome}`;
    if (!isMessageKey(key)) return t("task.outcome.unknown");
    const label = t(key);
    if (outcome !== "warnings" || !isUsableNumber(warnings) || warnings <= 0) {
      return label;
    }
    return `${label} (${String(Math.floor(warnings))})`;
  }

  /**
   * The legend of a chart: which window the curve under it covers.
   *
   * Written out rather than echoing the API word, which misreads in French —
   * "day" names a window of twenty-four hours, not a calendar day. The wording
   * is the one the mockups give the hour, extended to the other four in the
   * same shape.
   */
  function formatTimeframe(timeframe: Timeframe): string {
    return t(`timeframe.${timeframe}`);
  }

  /**
   * The same window on a button, where the legend would not fit: `24 h`, `7 j`.
   *
   * A duration rather than the PVE word, because the five options are read as
   * one scale and "mois" next to "semaine" does not say that one is four times
   * the other. The long form stays as the accessible name of the button, which
   * is what a screen reader announces.
   */
  function formatTimeframeShort(timeframe: Timeframe): string {
    return t(`timeframe.short.${timeframe}`);
  }

  return {
    formatBytes,
    formatUsage,
    formatUsageParts,
    formatUsageLine,
    formatRatio,
    formatCores,
    formatVcpus,
    formatLoadAverage,
    formatUptime,
    formatRelativeTime,
    formatNodeStatus,
    formatGuestStatus,
    formatStayingReason,
    formatGuestKind,
    formatQuorum,
    formatDateTime,
    formatInteger,
    plural,
    formatPendingUpdates,
    formatPackageCount: (count) => plural(count, "package"),
    formatDiskCount: (count) => plural(count, "disk"),
    formatNetCount: (count) => plural(count, "net"),
    formatMatchCount,
    formatAllocationQualifier,
    formatDetachedVolumes,
    formatHaState,
    formatErrorKind,
    formatMaintenanceError,
    formatMaintenanceOutcome,
    formatClusterStatus,
    formatAlert,
    formatTaskLabel,
    formatTaskOutcome,
    formatTimeframe,
    formatTimeframeShort,
  };
}
