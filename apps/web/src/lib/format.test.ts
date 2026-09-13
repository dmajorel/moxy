import { describe, expect, it } from "vitest";

import type { Alert, Allocation, ApiError, ApiErrorKind, Usage } from "@/api/types";
import {
  FALLBACK,
  NNBSP,
  formatAlert,
  formatAllocationQualifier,
  formatBytes,
  formatClusterStatus,
  formatCores,
  formatDetachedVolumes,
  formatDiskCount,
  formatErrorKind,
  formatGuestName,
  formatHaState,
  formatNodeStatus,
  formatPackageCount,
  formatPendingUpdates,
  formatRatio,
  formatRelativeTime,
  formatDateTime,
  formatGuestKind,
  formatGuestRef,
  formatGuestStatus,
  formatInteger,
  formatLoadAverage,
  formatQuorum,
  formatStayReason,
  formatTaskLabel,
  formatTaskOutcome,
  formatTaskTime,
  formatVcpus,
  plural,
  formatTime,
  formatUptime,
  formatUsage,
  formatVersionChange,
  splitTag,
} from "@/lib/format";

const KIB = 1024;
const MIB = 1024 * KIB;
const GIB = 1024 * MIB;
const TIB = 1024 * GIB;
const PIB = 1024 * TIB;

/** The ratio is not read by the formatter; it is kept honest anyway. */
function usage(used: number, total: number): Usage {
  return { used, total, ratio: total > 0 ? used / total : 0 };
}

/** A complete, exact allocation, which each case narrows to what it tests. */
function allocation(patch: Partial<Allocation> = {}): Allocation {
  return { bytes: 2076 * GIB, partial: false, detached: 0, detachedBytes: 0, ...patch };
}

describe("formatBytes", () => {
  it("renders the sizes of the mockups", () => {
    expect(formatBytes(1.2 * TIB)).toBe("1,2 TiB");
    expect(formatBytes(61 * GIB)).toBe("61 GiB");
    expect(formatBytes(418 * GIB)).toBe("418 GiB");
    expect(formatBytes(28 * GIB)).toBe("28 GiB");
    expect(formatBytes(5.4 * TIB)).toBe("5,4 TiB");
    expect(formatBytes(384 * GIB)).toBe("384 GiB");
  });

  it("drops the decimal at or above 100", () => {
    expect(formatBytes(212.4 * GIB)).toBe("212 GiB");
    expect(formatBytes(100.9 * GIB)).toBe("101 GiB");
  });

  it("keeps one decimal below 100 only when it is significant", () => {
    expect(formatBytes(99.94 * GIB)).toBe("99,9 GiB");
    expect(formatBytes(18 * GIB)).toBe("18 GiB");
    expect(formatBytes(3.9 * TIB)).toBe("3,9 TiB");
    expect(formatBytes(8 * TIB)).toBe("8 TiB");
    expect(formatBytes(1.5 * KIB)).toBe("1,5 KiB");
  });

  it("counts raw bytes below one KiB", () => {
    expect(formatBytes(0)).toBe("0 o");
    expect(formatBytes(1)).toBe("1 o");
    expect(formatBytes(512)).toBe("512 o");
    expect(formatBytes(1023)).toBe(`1${NNBSP}023 o`);
  });

  it("groups thousands with a narrow no-break space", () => {
    expect(formatBytes(1023 * GIB)).toBe(`1${NNBSP}023 GiB`);
    expect(formatBytes(2048 * PIB)).toBe(`2${NNBSP}048 PiB`);
  });

  it("promotes a value that rounds up to 1024", () => {
    expect(formatBytes(1024 * GIB)).toBe("1 TiB");
    expect(formatBytes(1023.7 * GIB)).toBe("1 TiB");
  });

  it("caps at the largest unit", () => {
    expect(formatBytes(3 * PIB)).toBe("3 PiB");
  });

  it("falls back on aberrant input", () => {
    expect(formatBytes(-1)).toBe(FALLBACK);
    expect(formatBytes(-GIB)).toBe(FALLBACK);
    expect(formatBytes(Number.NaN)).toBe(FALLBACK);
    expect(formatBytes(Number.POSITIVE_INFINITY)).toBe(FALLBACK);
    expect(formatBytes(Number.NEGATIVE_INFINITY)).toBe(FALLBACK);
  });
});

describe("formatUsage", () => {
  it("renders the four cluster cards of appendix A.4", () => {
    // Qualification
    expect(formatUsage(usage(61 * GIB, 384 * GIB))).toBe("61 / 384 GiB");
    expect(formatUsage(usage(1.2 * TIB, 5.4 * TIB))).toBe("1,2 / 5,4 TiB");
    // Préproduction
    expect(formatUsage(usage(212 * GIB, 256 * GIB))).toBe("212 / 256 GiB");
    expect(formatUsage(usage(3.9 * TIB, 8 * TIB))).toBe("3,9 / 8 TiB");
    // Production
    expect(formatUsage(usage(418 * GIB, 1024 * GIB))).toBe(`418 / 1${NNBSP}024 GiB`);
    expect(formatUsage(usage(14 * TIB, 32 * TIB))).toBe("14 / 32 TiB");
    // Node card of appendix A.2
    expect(formatUsage(usage(18 * GIB, 128 * GIB))).toBe("18 / 128 GiB");
  });

  it("expresses the total in the used value's unit rather than its own", () => {
    // 418 GiB / 1 TiB would force a mental conversion; 1 024 GiB does not.
    expect(formatUsage(usage(418 * GIB, 1024 * GIB))).toBe(`418 / 1${NNBSP}024 GiB`);
    expect(formatUsage(usage(412 * GIB, 1.8 * TIB))).toBe(`412 / 1${NNBSP}843 GiB`);
    expect(formatUsage(usage(980 * MIB, 8 * GIB))).toBe(`980 / 8${NNBSP}192 MiB`);
  });

  it("falls back on separate units beyond one unit step", () => {
    expect(formatUsage(usage(980 * MIB, 8 * TIB))).toBe("980 MiB / 8 TiB");
    expect(formatUsage(usage(1 * KIB, 8 * GIB))).toBe("1 KiB / 8 GiB");
    expect(formatUsage(usage(512, 4 * GIB))).toBe("512 o / 4 GiB");
  });

  it("never renders a non-zero usage as a flat zero", () => {
    expect(formatUsage(usage(980 * MIB, 8 * TIB))).not.toMatch(/^0 /);
    expect(formatUsage(usage(1 * KIB, 8 * GIB))).not.toMatch(/^0 /);
  });

  it("keeps the shared unit for a genuinely empty value", () => {
    expect(formatUsage(usage(0, 8 * TIB))).toBe("0 / 8 TiB");
    expect(formatUsage(usage(0, 256 * GIB))).toBe("0 / 256 GiB");
  });

  // A guest's boot disk is measured only when an agent reports it, and the
  // payload says so with a null used. "0 / 32 GiB" would claim an empty
  // volume, which a disk carrying a filesystem never is.
  it("renders the em dash when the used half is unknown", () => {
    expect(formatUsage({ used: null, total: 32 * GIB, ratio: null })).toBe(FALLBACK);
    expect(formatUsage({ used: 8 * GIB, total: 32 * GIB, ratio: 0.25 })).toBe(
      "8 / 32 GiB",
    );
  });

  it("falls back on aberrant input", () => {
    expect(formatUsage(usage(0, 0))).toBe(FALLBACK);
    // null is how the backend says "unknown", e.g. nodes it may not audit.
    expect(formatUsage(null)).toBe(FALLBACK);
    expect(formatUsage(undefined)).toBe(FALLBACK);
    expect(formatUsage(usage(1 * GIB, -1))).toBe(FALLBACK);
    expect(formatUsage(usage(-1, 8 * GIB))).toBe(FALLBACK);
    expect(formatUsage(usage(Number.NaN, 8 * GIB))).toBe(FALLBACK);
    expect(formatUsage(usage(1 * GIB, Number.POSITIVE_INFINITY))).toBe(FALLBACK);
  });
});

describe("formatRatio", () => {
  it("renders the ratios of the mockups", () => {
    expect(formatRatio(0.31)).toBe(`31${NNBSP}%`);
    expect(formatRatio(0.828)).toBe(`83${NNBSP}%`);
    expect(formatRatio(0.0025)).toBe(`0,25${NNBSP}%`);
    expect(formatRatio(0.031)).toBe(`3,1${NNBSP}%`);
    expect(formatRatio(0.04)).toBe(`4${NNBSP}%`);
    expect(formatRatio(0.22)).toBe(`22${NNBSP}%`);
  });

  it("separates the percent sign with a narrow no-break space", () => {
    expect(formatRatio(0.5)).toBe("50 %");
    expect(formatRatio(0.5)).not.toContain(" %");
  });

  it("honours an explicit precision", () => {
    expect(formatRatio(0.828, 1)).toBe(`82,8${NNBSP}%`);
    expect(formatRatio(0.828, 0)).toBe(`83${NNBSP}%`);
    expect(formatRatio(0.31, 2)).toBe(`31${NNBSP}%`);
  });

  it("never renders 0 % for a non-zero ratio", () => {
    expect(formatRatio(0.0004)).toBe(`<${NNBSP}0,1${NNBSP}%`);
    expect(formatRatio(1e-9)).toBe(`<${NNBSP}0,1${NNBSP}%`);
    expect(formatRatio(0.0025, 0)).toBe(`<${NNBSP}1${NNBSP}%`);
    expect(formatRatio(0.0004, 1)).toBe(`<${NNBSP}0,1${NNBSP}%`);
  });

  it("renders an unknown ratio as the fallback, never as 0 %", () => {
    expect(formatRatio(null)).toBe(FALLBACK);
    expect(formatRatio(undefined)).toBe(FALLBACK);
  });

  it("renders the bounds", () => {
    expect(formatRatio(0)).toBe(`0${NNBSP}%`);
    expect(formatRatio(1)).toBe(`100${NNBSP}%`);
    expect(formatRatio(1.5)).toBe(`150${NNBSP}%`);
  });

  it("falls back on aberrant input", () => {
    expect(formatRatio(-0.1)).toBe(FALLBACK);
    expect(formatRatio(Number.NaN)).toBe(FALLBACK);
    expect(formatRatio(Number.POSITIVE_INFINITY)).toBe(FALLBACK);
    expect(formatRatio(0.5, Number.NaN)).toBe(FALLBACK);
    expect(formatRatio(0.5, -1)).toBe(FALLBACK);
  });
});

describe("formatCores", () => {
  it("renders the counts of the mockups", () => {
    expect(formatCores(32)).toBe("32 c");
    expect(formatCores(6)).toBe("6 c");
  });

  it("groups the thousands like every other number", () => {
    expect(formatCores(1024)).toBe(`1${NNBSP}024 c`);
  });

  it("renders an unknown count as the fallback, never as 0 c", () => {
    expect(formatCores(null)).toBe(FALLBACK);
    expect(formatCores(undefined)).toBe(FALLBACK);
    expect(formatCores(0)).toBe(FALLBACK);
  });

  it("falls back on aberrant input", () => {
    expect(formatCores(-4)).toBe(FALLBACK);
    expect(formatCores(Number.NaN)).toBe(FALLBACK);
    expect(formatCores(Number.POSITIVE_INFINITY)).toBe(FALLBACK);
  });

  it("rounds a fractional count rather than writing a decimal", () => {
    expect(formatCores(31.6)).toBe("32 c");
  });
});

describe("formatUptime", () => {
  it("renders the uptimes of the mockups", () => {
    expect(formatUptime(41 * 86400)).toBe("41 j");
    expect(formatUptime(2 * 86400 + 22 * 3600)).toBe("2 j 22 h");
    expect(formatUptime(3 * 3600 + 14 * 60)).toBe("3 h 14 min");
    expect(formatUptime(47 * 60)).toBe("47 min");
    expect(formatUptime(12)).toBe("12 s");
  });

  it("shows two units at most, largest first", () => {
    expect(formatUptime(2 * 86400 + 22 * 3600 + 33 * 60 + 44)).toBe("2 j 22 h");
    expect(formatUptime(3 * 3600 + 14 * 60 + 9)).toBe("3 h 14 min");
    expect(formatUptime(47 * 60 + 9)).toBe("47 min 9 s");
  });

  it("never writes a zero unit", () => {
    expect(formatUptime(86400)).toBe("1 j");
    expect(formatUptime(86400 + 60)).toBe("1 j");
    expect(formatUptime(3600)).toBe("1 h");
    expect(formatUptime(3600 + 9)).toBe("1 h");
    expect(formatUptime(60)).toBe("1 min");
  });

  it("renders the bounds", () => {
    expect(formatUptime(0)).toBe("0 s");
    expect(formatUptime(0.4)).toBe("0 s");
    expect(formatUptime(59)).toBe("59 s");
    expect(formatUptime(86399)).toBe("23 h 59 min");
  });

  // null is the backend saying "there is no uptime here": an offline node, a
  // stopped guest, a node the token may not audit. The em dash says so; a
  // "0 s" would claim the machine came up this very second, which is the
  // opposite of what happened.
  it("renders the em dash when there is no uptime to report", () => {
    expect(formatUptime(null)).toBe(FALLBACK);
    expect(formatUptime(null)).not.toBe("0 s");
  });

  it("falls back on aberrant input", () => {
    expect(formatUptime(-1)).toBe(FALLBACK);
    expect(formatUptime(Number.NaN)).toBe(FALLBACK);
    expect(formatUptime(Number.POSITIVE_INFINITY)).toBe(FALLBACK);
  });
});

describe("formatRelativeTime", () => {
  const now = new Date("2026-02-03T12:00:00.000Z");
  const ago = (seconds: number) => new Date(now.getTime() - seconds * 1000);

  it("renders the freshness of the data", () => {
    expect(formatRelativeTime(ago(0), now)).toBe("à l'instant");
    expect(formatRelativeTime(ago(4), now)).toBe("à l'instant");
    expect(formatRelativeTime(ago(12), now)).toBe("il y a 12 s");
    expect(formatRelativeTime(ago(3 * 60), now)).toBe("il y a 3 min");
    expect(formatRelativeTime(ago(2 * 3600), now)).toBe("il y a 2 h");
    expect(formatRelativeTime(ago(3 * 86400), now)).toBe("il y a 3 j");
  });

  it("renders the unit bounds", () => {
    expect(formatRelativeTime(ago(5), now)).toBe("il y a 5 s");
    expect(formatRelativeTime(ago(59), now)).toBe("il y a 59 s");
    expect(formatRelativeTime(ago(60), now)).toBe("il y a 1 min");
    expect(formatRelativeTime(ago(3599), now)).toBe("il y a 59 min");
    expect(formatRelativeTime(ago(3600), now)).toBe("il y a 1 h");
    expect(formatRelativeTime(ago(86399), now)).toBe("il y a 23 h");
    expect(formatRelativeTime(ago(86400), now)).toBe("il y a 1 j");
  });

  it("treats a future date as fresh, clock skew being routine", () => {
    expect(formatRelativeTime(ago(-30), now)).toBe("à l'instant");
  });

  it("defaults to the current time", () => {
    expect(formatRelativeTime(new Date())).toBe("à l'instant");
  });

  it("falls back on an invalid date", () => {
    expect(formatRelativeTime(new Date("nope"), now)).toBe(FALLBACK);
    expect(formatRelativeTime(now, new Date("nope"))).toBe(FALLBACK);
  });
});

describe("formatGuestName", () => {
  it("returns the name whole, with neither vmid nor segment stripping", () => {
    expect(formatGuestName(103, "sli-airflow-sep-exp-2601-qul")).toBe(
      "sli-airflow-sep-exp-2601-qul",
    );
    expect(formatGuestName(100, "sli-testproxmox-qul")).toBe("sli-testproxmox-qul");
    expect(formatGuestName(102, "sli-testproxmox-2-qul")).toBe("sli-testproxmox-2-qul");
  });

  it("renders off-convention names untouched", () => {
    // Nothing here follows `<prefix>-<segments>-<number>-<environment>`, and
    // nothing here may be rewritten to look as if it did.
    expect(formatGuestName(101, "template-rocky10")).toBe("template-rocky10");
    expect(formatGuestName(200, "gitlab_runner")).toBe("gitlab_runner");
    expect(formatGuestName(200, "win2022.corp")).toBe("win2022.corp");
  });

  it("trims the surrounding whitespace PVE sometimes keeps", () => {
    expect(formatGuestName(200, "  db  ")).toBe("db");
  });

  it("falls back to the vmid for a nameless guest, then to the em dash", () => {
    expect(formatGuestName(103, "")).toBe("103");
    expect(formatGuestName(103, "   ")).toBe("103");
    expect(formatGuestName(Number.NaN, "")).toBe(FALLBACK);
  });
});

describe("splitTag", () => {
  it("cuts at the LAST dot, so the key is the whole namespace", () => {
    // "ha" with a value of "state.started" would be a different, and wrong,
    // reading of the same tag.
    expect(splitTag("ha.state.started")).toEqual({ key: "ha.state", value: "started" });
  });

  it("reads a one-dot tag as the pair it is", () => {
    expect(splitTag("env.qualification")).toEqual({ key: "env", value: "qualification" });
    expect(splitTag("backup.none")).toEqual({ key: "backup", value: "none" });
    expect(splitTag("date.20260907")).toEqual({ key: "date", value: "20260907" });
  });

  it("keeps a dotless flag tag whole, with nothing to qualify it", () => {
    // PVE imposes no structure: "production" names without qualifying.
    expect(splitTag("production")).toEqual({ key: "production", value: null });
  });

  it("refuses to amputate a tag whose cut would leave a half empty", () => {
    expect(splitTag(".foo")).toEqual({ key: ".foo", value: null });
    expect(splitTag("foo.")).toEqual({ key: "foo.", value: null });
    expect(splitTag(".")).toEqual({ key: ".", value: null });
    expect(splitTag("")).toEqual({ key: "", value: null });
  });
});

describe("formatNodeStatus", () => {
  it("renders every status in sentence case", () => {
    expect(formatNodeStatus("online")).toBe("En ligne");
    expect(formatNodeStatus("offline")).toBe("Hors ligne");
    expect(formatNodeStatus("maintenance")).toBe("Maintenance");
    expect(formatNodeStatus("unknown")).toBe("Inconnu");
  });

  it("falls back on an unexpected status", () => {
    expect(formatNodeStatus("bogus" as never)).toBe("Inconnu");
  });
});

describe("formatPendingUpdates", () => {
  it("tells an up-to-date node from one nobody could ask about", () => {
    expect(formatPendingUpdates(0)).toBe("À jour");
    expect(formatPendingUpdates(null)).toBeNull();
  });

  it("counts the pending updates", () => {
    expect(formatPendingUpdates(1)).toBe("1 en attente");
    expect(formatPendingUpdates(12)).toBe("12 en attente");
  });
});

describe("formatPackageCount", () => {
  it("agrees the plural", () => {
    expect(formatPackageCount(1)).toBe("1 paquet");
    expect(formatPackageCount(12)).toBe("12 paquets");
  });
});

describe("formatDiskCount", () => {
  it("agrees the plural", () => {
    expect(formatDiskCount(1)).toBe("1 disque");
    expect(formatDiskCount(4)).toBe("4 disques");
  });
});

describe("formatAllocationQualifier", () => {
  it("says the total is exact when every volume declares a size", () => {
    expect(formatAllocationQualifier(allocation())).toBe("· alloué");
  });

  it("says the total is a floor when one volume declares none", () => {
    // A device passed straight through carries no size. The sum keeps what it
    // knows and must not claim to be the whole truth.
    expect(formatAllocationQualifier(allocation({ partial: true }))).toBe("· au moins");
  });

  it("has nothing to qualify when the configuration could not be read", () => {
    expect(formatAllocationQualifier(null)).toBeNull();
  });
});

describe("formatDetachedVolumes", () => {
  it("draws no line when nothing was left behind", () => {
    expect(formatDetachedVolumes(allocation())).toBeNull();
    expect(formatDetachedVolumes(null)).toBeNull();
  });

  it("agrees the plural and appends a size only when one is known", () => {
    expect(formatDetachedVolumes(allocation({ detached: 1 }))).toBe("1 volume détaché");
    expect(formatDetachedVolumes(allocation({ detached: 3 }))).toBe("3 volumes détachés");
    expect(formatDetachedVolumes(allocation({ detached: 1, detachedBytes: 8 * GIB }))).toBe(
      "1 volume détaché (8 GiB)",
    );
  });
});

describe("formatVersionChange", () => {
  it("puts the installed version before the pending one", () => {
    expect(formatVersionChange("9.2.11", "9.2.12")).toBe("9.2.11 → 9.2.12");
  });

  it("shows the new version alone for a package apt would add", () => {
    expect(formatVersionChange(null, "1.2.0")).toBe("1.2.0");
  });
});

describe("formatClusterStatus", () => {
  it("renders every status in sentence case", () => {
    expect(formatClusterStatus("healthy")).toBe("Sain");
    expect(formatClusterStatus("degraded")).toBe("Dégradé");
    expect(formatClusterStatus("unreachable")).toBe("Injoignable");
  });

  it("falls back on an unexpected status", () => {
    expect(formatClusterStatus("bogus" as never)).toBe("Inconnu");
  });
});

describe("formatAlert", () => {
  it("renders the banners of the mockups", () => {
    expect(
      formatAlert({ kind: "memory_high", ratio: 0.828, nodes: ["a", "b"] }),
    ).toBe(`Mémoire à 83${NNBSP}% sur 2 nœuds (max.)`);
    expect(
      formatAlert({
        kind: "updates_available",
        version: "9.2.12",
        nodes: ["a", "b", "c", "d", "e"],
      }),
    ).toBe("Mise à jour 9.2.12 disponible sur 5 nœuds");
    expect(formatAlert({ kind: "quorum_lost" })).toBe("Quorum perdu");
    expect(formatAlert({ kind: "node_offline", nodes: ["a", "b"] })).toBe(
      "2 nœuds hors ligne",
    );
    expect(formatAlert({ kind: "unreachable" })).toBe("Cluster injoignable");
    expect(formatAlert({ kind: "node_unknown", nodes: ["a", "b"] })).toBe(
      "2 nœuds dans un état inconnu",
    );
    expect(
      formatAlert({ kind: "node_stats_unavailable", nodes: ["a", "b", "c", "d", "e", "f"] }),
    ).toBe("Mesures CPU et mémoire indisponibles sur 6 nœuds");
    expect(formatAlert({ kind: "node_stats_unavailable" })).toBe(
      "Mesures CPU et mémoire indisponibles",
    );
    expect(
      formatAlert({ kind: "updates_uneven", pendingMin: 11, pendingMax: 14 }),
    ).toBe("Mises à jour inégales : de 11 à 14 paquets en attente selon les nœuds");
  });

  it("agrees in number", () => {
    expect(formatAlert({ kind: "node_offline", nodes: ["a"] })).toBe(
      "1 nœud hors ligne",
    );
    expect(formatAlert({ kind: "memory_high", ratio: 0.9, nodes: ["a"] })).toBe(
      `Mémoire à 90${NNBSP}% sur 1 nœud (max.)`,
    );
    expect(formatAlert({ kind: "node_unknown", nodes: ["a"] })).toBe(
      "1 nœud dans un état inconnu",
    );
    expect(
      formatAlert({ kind: "updates_available", version: "9.2.12", nodes: ["a"] }),
    ).toBe("Mise à jour 9.2.12 disponible sur 1 nœud");
  });

  // A cluster at 55 % holding one node at 92 % used to render "Mémoire à
  // 55 % sur 1 nœud", which states the cluster average of the only node it
  // names. The backend now sends the node's own ratio, and the label says
  // which figure it is rather than leaving the reader to guess.
  it("says which memory figure it is showing", () => {
    expect(formatAlert({ kind: "memory_high", ratio: 0.92, nodes: ["node-3"] })).toBe(
      `Mémoire à 92${NNBSP}% sur 1 nœud (max.)`,
    );
    // No node over the threshold: the cluster as a whole is full, the ratio
    // is its own, and there is nothing to qualify.
    expect(formatAlert({ kind: "memory_high", ratio: 0.83 })).toBe(
      `Mémoire à 83${NNBSP}%`,
    );
  });

  // "hors ligne" is a claim the cluster made; "inconnu" is the absence of one.
  it("does not call an unknown node offline", () => {
    expect(formatAlert({ kind: "node_unknown", nodes: ["node-4"] })).not.toContain(
      "hors ligne",
    );
  });

  it("degrades when an optional field is missing", () => {
    expect(formatAlert({ kind: "memory_high", ratio: 0.828 })).toBe(
      `Mémoire à 83${NNBSP}%`,
    );
    expect(formatAlert({ kind: "memory_high", nodes: ["a", "b"] })).toBe(
      "Mémoire élevée sur 2 nœuds",
    );
    expect(formatAlert({ kind: "memory_high" })).toBe("Mémoire élevée");
    expect(formatAlert({ kind: "updates_available", nodes: ["a", "b"] })).toBe(
      "Mise à jour disponible sur 2 nœuds",
    );
    expect(formatAlert({ kind: "updates_available", version: "9.2.12" })).toBe(
      "Mise à jour 9.2.12 disponible",
    );
    expect(formatAlert({ kind: "updates_available" })).toBe("Mise à jour disponible");
    expect(formatAlert({ kind: "node_offline" })).toBe("Nœud hors ligne");
    expect(formatAlert({ kind: "node_offline", nodes: [] })).toBe("Nœud hors ligne");
    expect(formatAlert({ kind: "node_unknown" })).toBe("Nœud dans un état inconnu");
    expect(formatAlert({ kind: "node_unknown", nodes: [] })).toBe(
      "Nœud dans un état inconnu",
    );
    expect(formatAlert({ kind: "updates_uneven" })).toBe(
      "Mises à jour inégales entre les nœuds",
    );
    expect(formatAlert({ kind: "updates_uneven", pendingMin: 11 })).toBe(
      "Mises à jour inégales entre les nœuds",
    );
    expect(
      formatAlert({ kind: "updates_uneven", pendingMin: 14, pendingMax: 14 }),
    ).toBe("Mises à jour inégales entre les nœuds");
  });

  // The alert is about the spread between nodes, not about a set of them:
  // "sur 1 nœud" would read as "the problem is that node".
  it("never counts nodes on an uneven-updates alert", () => {
    expect(
      formatAlert({
        kind: "updates_uneven",
        pendingMin: 8,
        pendingMax: 14,
        nodes: ["a", "b", "c"],
      }),
    ).toBe("Mises à jour inégales : de 8 à 14 paquets en attente selon les nœuds");
  });

  it("falls back on an unexpected alert", () => {
    expect(formatAlert({ kind: "bogus" } as unknown as Alert)).toBe("Alerte");
    expect(formatAlert({ kind: "memory_high", ratio: Number.NaN })).toBe(
      "Mémoire élevée",
    );
    expect(formatAlert({ kind: "memory_high", ratio: -1 })).toBe("Mémoire élevée");
  });
});

describe("formatTime", () => {
  it("renders the clock time of a timestamp", () => {
    const iso = new Date(2026, 8, 12, 12, 0, 2).toISOString();
    expect(formatTime(iso)).toBe("12:00:02");
  });

  it("pads every field", () => {
    const iso = new Date(2026, 8, 12, 4, 5, 6).toISOString();
    expect(formatTime(iso)).toBe("04:05:06");
  });

  it("falls back on an unparseable timestamp", () => {
    expect(formatTime("not a date")).toBe(FALLBACK);
  });
});

describe("formatTaskLabel", () => {
  it("names a known task type in French and appends its subject", () => {
    expect(
      formatTaskLabel({ type: "vzdump", id: "103", node: "prox-qual-2201-cit" }),
    ).toBe("Sauvegarde · 103");
  });

  it("falls back to the node when the id adds nothing", () => {
    expect(
      formatTaskLabel({ type: "aptupdate", id: "", node: "prox-qual-2201-cit" }),
    ).toBe("Mise à jour des paquets · prox-qual-2201-cit");
    expect(
      formatTaskLabel({
        type: "srvreload",
        id: "prox-qual-2201-cit",
        node: "prox-qual-2201-cit",
      }),
    ).toBe("Rechargement du service · prox-qual-2201-cit");
  });

  it("keeps an unknown type verbatim rather than hiding it", () => {
    // An operator can search for the raw word; a generic label loses it.
    expect(
      formatTaskLabel({ type: "zfsscrub", id: "tank", node: "pve-01" }),
    ).toBe("zfsscrub · tank");
  });
});

describe("formatTaskOutcome", () => {
  it("names the four outcomes", () => {
    expect(formatTaskOutcome("running")).toBe("En cours");
    expect(formatTaskOutcome("ok")).toBe("OK");
    expect(formatTaskOutcome("failed")).toBe("Échec");
    expect(formatTaskOutcome("warnings")).toBe("Avertissements");
  });

  // Two warnings on a backup of ninety guests is not the same news as thirty,
  // so the count is worth the parenthesis.
  it("appends the warning count when the backend could read one", () => {
    expect(formatTaskOutcome("warnings", 2)).toBe("Avertissements (2)");
    expect(formatTaskOutcome("warnings", 1)).toBe("Avertissements (1)");
  });

  // No count, no parenthesis around nothing.
  it("stands alone when there is no count", () => {
    expect(formatTaskOutcome("warnings", null)).toBe("Avertissements");
    expect(formatTaskOutcome("warnings", 0)).toBe("Avertissements");
    expect(formatTaskOutcome("warnings", Number.NaN)).toBe("Avertissements");
  });

  // A count on any other outcome is not a warning count: it is ignored rather
  // than rendered as "OK (2)".
  it("ignores a count on the other outcomes", () => {
    expect(formatTaskOutcome("ok", 2)).toBe("OK");
    expect(formatTaskOutcome("failed", 2)).toBe("Échec");
  });
});

describe("formatHaState", () => {
  // The backend serves the CRM's own word because that is what an operator
  // needs during an incident: "fence" means the cluster is isolating a node.
  it("translates the states the crm publishes", () => {
    expect(formatHaState("started")).toBe("Démarré");
    expect(formatHaState("error")).toBe("Erreur");
    expect(formatHaState("fence")).toBe("Isolation");
    expect(formatHaState("migrate")).toBe("Migration");
    expect(formatHaState("maintenance")).toBe("En maintenance");
  });

  it("shows an unknown state as it came rather than hiding it", () => {
    expect(formatHaState("some-new-pve-state")).toBe("some-new-pve-state");
  });

  // null is "not an HA resource, or no HA manager": KeyValue renders the em
  // dash for it, which is the same thing said in the interface's own terms.
  it("returns null for an absent state", () => {
    expect(formatHaState(null)).toBeNull();
    expect(formatHaState("")).toBeNull();
    expect(formatHaState(undefined)).toBeNull();
  });
});

describe("formatErrorKind", () => {
  const failure = (kind: ApiErrorKind, status: number | null = null): ApiError => ({
    kind,
    status,
    message: "diagnostic text that never reaches the interface",
  });

  it("names every class the backend reports", () => {
    expect(formatErrorKind(failure("tls"))).toBe("certificat non vérifiable");
    expect(formatErrorKind(failure("timeout"))).toBe("délai dépassé");
    expect(formatErrorKind(failure("network"))).toBe("réseau injoignable");
    expect(formatErrorKind(failure("protocol"))).toBe("réponse inattendue");
  });

  // "auth" covers two opposite errands, and the status is what tells them apart.
  it("separates a revoked token from a missing privilege", () => {
    expect(formatErrorKind(failure("auth", 401))).toBe("jeton refusé");
    expect(formatErrorKind(failure("auth", 403))).toBe("droits insuffisants");
    expect(formatErrorKind(failure("auth"))).toBe("authentification refusée");
  });

  it("returns null when there is no error", () => {
    expect(formatErrorKind(null)).toBeNull();
    expect(formatErrorKind(undefined)).toBeNull();
  });
});

describe("formatLoadAverage", () => {
  // It goes through formatNumber like every other figure, so the decimal
  // comma, the grouping and the rounding are decided in one place. A toFixed
  // written in a screen drifted the day any of the three changed.
  it("renders the three figures of the mockup", () => {
    expect(formatLoadAverage([0.84, 0.91, 0.88])).toBe("0,84 · 0,91 · 0,88");
  });

  // A quiet node really reports zeroes; turning that into "unknown" would be
  // the symmetrical lie.
  it("keeps a genuine zero", () => {
    expect(formatLoadAverage([0, 0, 0])).toBe("0 · 0 · 0");
  });

  it("groups a load a sysadmin hopes never to see", () => {
    expect(formatLoadAverage([1234.5, 2, 3])).toBe(`1${NNBSP}234,5 · 2 · 3`);
  });

  it("falls back when there is nothing to report", () => {
    expect(formatLoadAverage(null)).toBe(FALLBACK);
    expect(formatLoadAverage(undefined)).toBe(FALLBACK);
    expect(formatLoadAverage([Number.NaN, 1, 1])).toBe(FALLBACK);
    expect(formatLoadAverage([-1, 1, 1])).toBe(FALLBACK);
  });
});

describe("formatQuorum", () => {
  it("says the verdict and the votes", () => {
    expect(formatQuorum({ quorate: true, nodes: 3, online: 3 })).toBe("OK · 3/3 votes");
    expect(formatQuorum({ quorate: false, nodes: 3, online: 1 })).toBe(
      "Perdu · 1/3 votes",
    );
  });

  // A standalone node has no quorum at all. Inventing a one-node vote would
  // put a healthy machine in a state it is not in, which is why this is a
  // sentence of its own rather than the dash.
  it("names the standalone node rather than showing a dash", () => {
    expect(formatQuorum(null)).toBe("Nœud seul");
    expect(formatQuorum(undefined)).toBe("Nœud seul");
  });
});

describe("formatVcpus", () => {
  it("renders the count, invariable as PVE writes it", () => {
    expect(formatVcpus(4)).toBe("4 vCPU");
    expect(formatVcpus(1)).toBe("1 vCPU");
  });

  // PVE always assigns at least one, so a zero is a figure that never arrived.
  it("falls back on an impossible count", () => {
    expect(formatVcpus(0)).toBe(FALLBACK);
    expect(formatVcpus(null)).toBe(FALLBACK);
    expect(formatVcpus(Number.NaN)).toBe(FALLBACK);
  });
});

describe("formatGuestStatus", () => {
  // "template", "Template" and "Modèle" all named this state in different
  // files, which reads as three states.
  it("uses one word per state, in French", () => {
    expect(formatGuestStatus("running")).toBe("En cours");
    expect(formatGuestStatus("stopped")).toBe("Arrêtée");
    expect(formatGuestStatus("template")).toBe("Modèle");
  });
});

describe("formatStayReason", () => {
  // The same word as everywhere else: a guest that stays behind because it is
  // a template must not be called something a fourth way.
  it("reads a template as the interface reads it everywhere", () => {
    expect(formatStayReason("template")).toBe(formatGuestStatus("template"));
  });

  // A key nobody has translated shows as it came rather than disappearing: an
  // untranslated word is a bug report, an empty cell is a mystery.
  it("passes an unknown key through", () => {
    expect(formatStayReason("pinned")).toBe("pinned");
  });
});

describe("formatGuestKind and formatGuestRef", () => {
  // A breadcrumb calling an LXC "VM 105" contradicts pct and the native
  // interface, which both write CT.
  it("names a container a container", () => {
    expect(formatGuestKind("lxc")).toBe("Conteneur LXC");
    expect(formatGuestKind("qemu")).toBe("Machine virtuelle");
    expect(formatGuestRef("lxc", 105)).toBe("CT 105");
    expect(formatGuestRef("qemu", 103)).toBe("VM 103");
  });

  it("keeps the prefix when the id is unusable", () => {
    expect(formatGuestRef("qemu", Number.NaN)).toBe("VM");
  });
});

describe("formatDateTime", () => {
  it("renders the stamp of the staleness banner", () => {
    expect(formatDateTime(new Date(2026, 8, 12, 14, 32))).toBe("12/09/2026 à 14:32");
  });

  // Null rather than "Invalid Date": the caller writes a sentence with no
  // timestamp at all.
  it("yields null for a date it cannot read", () => {
    expect(formatDateTime(null)).toBeNull();
    expect(formatDateTime(new Date("nonsense"))).toBeNull();
  });
});

describe("plural and formatInteger", () => {
  it("pluralises from two, as French does", () => {
    expect(plural(1, "nœud", "nœuds")).toBe("1 nœud");
    expect(plural(2, "nœud", "nœuds")).toBe("2 nœuds");
    expect(plural(0, "nœud", "nœuds")).toBe("0 nœuds");
  });

  it("groups the thousands like every other figure", () => {
    expect(plural(1024, "VM", "VM")).toBe(`1${NNBSP}024 VM`);
    expect(formatInteger(1024)).toBe(`1${NNBSP}024`);
    expect(formatInteger(Number.NaN)).toBe(FALLBACK);
  });
});

describe("formatTaskTime", () => {
  const now = new Date(2026, 8, 12, 14, 0, 0);

  // The clock alone was enough for the sample data, which spans an evening.
  // In production, nightly backups spread twenty-five tasks over several days,
  // and "04:26:34" from the day before yesterday looks exactly like
  // "04:26:34" from last night.
  it("writes the clock alone for a task of today", () => {
    expect(formatTaskTime(new Date(2026, 8, 12, 4, 26, 34).toISOString(), now)).toBe(
      "04:26:34",
    );
  });

  it("adds the date for any other day", () => {
    expect(formatTaskTime(new Date(2026, 8, 11, 4, 26, 34).toISOString(), now)).toBe(
      "11/09 04:26:34",
    );
  });

  it("adds the year when it differs", () => {
    expect(formatTaskTime(new Date(2025, 8, 11, 4, 26, 34).toISOString(), now)).toBe(
      "11/09/2025 04:26:34",
    );
  });

  // A task dated tomorrow is a clock skew between two nodes, not a task that
  // has not happened yet. It is shown with its date like any other.
  it("dates a task from the future too", () => {
    expect(formatTaskTime(new Date(2026, 8, 13, 1, 0, 0).toISOString(), now)).toBe(
      "13/09 01:00:00",
    );
  });

  it("falls back on an unreadable timestamp", () => {
    expect(formatTaskTime("nonsense", now)).toBe(FALLBACK);
    expect(formatTaskTime("", now)).toBe(FALLBACK);
  });
});

describe("formatTaskLabel, on the types PVE actually emits", () => {
  // A type missing from the table falls back to its raw name, which is English
  // in a French interface: "qmresume · 103" in a column of sentences.
  it.each([
    ["qmresume", "Reprise"],
    ["qmsuspend", "Suspension"],
    ["qmtemplate", "Conversion en modèle"],
    ["qmrestore", "Restauration"],
    ["qmdelsnapshot", "Suppression d’instantané".replace("’", "'")],
    ["qmrollback", "Retour à un instantané"],
    ["qmmove", "Déplacement de disque"],
    ["vzrestore", "Restauration"],
    ["vzclone", "Clonage"],
    ["hastart", "Démarrage HA"],
    ["resize", "Redimensionnement"],
    ["acmerenew", "Renouvellement ACME"],
    ["cephcreateosd", "Création d’OSD Ceph".replace("’", "'")],
    ["clusterjoin", "Adhésion au cluster"],
    ["wipedisk", "Effacement de disque"],
  ])("names %s in French", (type, label) => {
    expect(formatTaskLabel({ type, id: "103", node: "pve-1" })).toBe(`${label} · 103`);
  });

  // The fallback stays: an operator can search for the raw word, which a
  // generic "Tâche" would have hidden.
  it("keeps an unknown type verbatim", () => {
    expect(formatTaskLabel({ type: "zfsscrub", id: "tank", node: "pve-1" })).toBe(
      "zfsscrub · tank",
    );
  });
});
