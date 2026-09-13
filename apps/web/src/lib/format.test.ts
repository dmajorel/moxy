import { describe, expect, it } from "vitest";

import type { Alert, Usage } from "@/api/types";
import {
  FALLBACK,
  NNBSP,
  formatAlert,
  formatBytes,
  formatClusterStatus,
  formatCores,
  formatGuestName,
  formatNodeStatus,
  formatPackageCount,
  formatPendingUpdates,
  formatRatio,
  formatRelativeTime,
  formatTaskLabel,
  formatTime,
  formatUptime,
  formatUsage,
  formatVersionChange,
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
    ).toBe(`Mémoire à 83${NNBSP}% sur 2 nœuds`);
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
      `Mémoire à 90${NNBSP}% sur 1 nœud`,
    );
    expect(
      formatAlert({ kind: "updates_available", version: "9.2.12", nodes: ["a"] }),
    ).toBe("Mise à jour 9.2.12 disponible sur 1 nœud");
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
