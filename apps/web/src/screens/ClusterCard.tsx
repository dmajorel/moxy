/**
 * One cluster, summarised — the unit of screen 4 (appendix A.4 of the handoff).
 *
 * The card answers three questions without a click: is this cluster healthy,
 * how loaded is it, and what is the one thing worth looking at. Everything it
 * displays comes from `@/lib/format`; no unit, no percentage and no French
 * plural rule for a *value* is rebuilt here.
 */
import type { KeyboardEvent } from "react";

import type {
  Alert,
  ClusterOverview,
  ClusterStatus,
  Node,
  VmCounts,
} from "@/api/types";
import type { AlertBannerIcon, TagVariant } from "@/components/ui";
import { AlertBanner, StatusDot, Tag, UsageBar } from "@/components/ui";
import {
  formatAlert,
  formatClusterStatus,
  formatNodeStatus,
  formatRatio,
  formatRelativeTime,
  formatUsage,
} from "@/lib/format";

export interface ClusterCardProps {
  cluster: ClusterOverview;
  /** Ratio above which a usage bar turns amber. Owned by the API payload. */
  threshold: number;
  /** When given, the whole card becomes a keyboard-operable control. */
  onSelect?: () => void;
  className?: string;
}

const STATUS_TAG_VARIANT: Record<ClusterStatus, TagVariant> = {
  healthy: "success",
  degraded: "warning",
  unreachable: "neutral",
};

/**
 * Border treatment per status.
 *
 * `degraded` is the 2px amber frame of the mock. `unreachable` deliberately
 * stays out of amber: section 2 reserves that colour for degradation and for
 * actions with consequences — something the operator can act on *now* — while
 * an unreachable cluster says only "this reading can no longer be confirmed".
 * So it reuses the muted tone already carried by its own StatusDot, as a dashed
 * hairline: unmistakably different from both the plain healthy card and the
 * amber degraded one, without inventing a fourth colour.
 */
const STATUS_BORDER_CLASSES: Record<ClusterStatus, string> = {
  healthy: "border-[0.5px] border-border",
  degraded: "border-2 border-warning",
  unreachable: "border-[0.5px] border-dashed border-text-muted",
};

/** The mock closes the update alert with a refresh glyph, not a warning one. */
function bannerIcon(alert: Alert): AlertBannerIcon {
  return alert.kind === "updates_available" ? "refresh" : "alert";
}

/**
 * Builds `12 en cours · 1 template` from the counters, keeping only the terms
 * that carry something. An all-zero cluster says so rather than showing a blank.
 */
function vmSummary(vms: VmCounts): string {
  const parts: string[] = [];
  if (vms.running > 0) {
    parts.push(`${vms.running} en cours`);
  }
  if (vms.stopped > 0) {
    parts.push(`${vms.stopped} ${vms.stopped === 1 ? "arrêtée" : "arrêtées"}`);
  }
  if (vms.templates > 0) {
    parts.push(`${vms.templates} ${vms.templates === 1 ? "template" : "templates"}`);
  }
  return parts.length > 0 ? parts.join(" · ") : "Aucune VM";
}

/**
 * The freshness line.
 *
 * `error.message` is an English diagnostic (see CLAUDE.md) and never reaches
 * this string: a failed poll is told as "this is an old reading", which is the
 * only part of it the operator can act on.
 */
function freshnessLabel(cluster: ClusterOverview): string | null {
  const relative =
    cluster.fetchedAt === null
      ? null
      : formatRelativeTime(new Date(cluster.fetchedAt));

  if (cluster.error !== null) {
    return relative === null
      ? "Aucune lecture disponible"
      : `Lecture ancienne · ${relative}`;
  }
  return relative;
}

/** Footer sentence when nothing is wrong: the quorum, or nothing at all. */
function quietBanner(cluster: ClusterOverview): string {
  if (cluster.quorum === null) {
    // Standalone node: it has no quorum, so none is invented.
    return "Aucune alerte";
  }
  return `Quorum ${cluster.quorum.online}/${cluster.quorum.nodes} · aucune alerte`;
}

interface MetricRowProps {
  label: string;
  value: string;
  ratio: number;
  threshold: number;
}

/** Label left, value right, fill bar underneath — the `.m` + `.bar` pair. */
function MetricRow({ label, value, ratio, threshold }: MetricRowProps) {
  return (
    <div>
      <div className="flex items-baseline justify-between py-[5px] text-[12px]">
        <span className="text-text-secondary">{label}</span>
        <span className="text-text-primary">{value}</span>
      </div>
      <UsageBar
        className="mt-[2px] mb-2"
        ratio={ratio}
        size={4}
        threshold={threshold}
        label={label}
      />
    </div>
  );
}

const NODE_ROW_CLASSES =
  "flex items-center gap-1.5 border-t-[0.5px] border-border py-[5px] text-[12px]";

function NodeRow({ node }: { node: Node }) {
  return (
    <li className={NODE_ROW_CLASSES}>
      <StatusDot status={node.status} />
      <span className="truncate text-text-primary">{node.name}</span>
      {node.status === "maintenance" ? (
        <Tag className="ml-auto" variant="warning">
          {formatNodeStatus(node.status)}
        </Tag>
      ) : null}
    </li>
  );
}

const BASE_CLASSES = "rounded-panel bg-surface-2 px-4 py-3.5 text-left";

const INTERACTIVE_CLASSES =
  "cursor-pointer hover:bg-surface-1 focus-visible:outline " +
  "focus-visible:outline-1 focus-visible:outline-offset-1 " +
  "focus-visible:outline-accent";

export function ClusterCard({
  cluster,
  threshold,
  onSelect,
  className,
}: ClusterCardProps) {
  const interactive = onSelect !== undefined;
  const alert = cluster.alerts[0];
  const freshness = freshnessLabel(cluster);

  // A div carrying role="button" rather than a <button>: the card holds a
  // heading and lists, which a <button> may not contain. Activation is wired by
  // hand so that Enter and Space behave as the role promises.
  const handleKeyDown = (event: KeyboardEvent<HTMLElement>) => {
    if (onSelect === undefined) {
      return;
    }
    if (event.key === "Enter" || event.key === " ") {
      event.preventDefault();
      onSelect();
    }
  };

  const classes = [
    BASE_CLASSES,
    STATUS_BORDER_CLASSES[cluster.status],
    interactive ? INTERACTIVE_CLASSES : "",
    className,
  ]
    .filter(Boolean)
    .join(" ");

  return (
    <article
      className={classes}
      role={interactive ? "button" : undefined}
      tabIndex={interactive ? 0 : undefined}
      aria-label={interactive ? `Cluster ${cluster.name}` : undefined}
      onClick={onSelect}
      onKeyDown={interactive ? handleKeyDown : undefined}
    >
      <div className="mb-2.5 flex items-center gap-2">
        <StatusDot status={cluster.status} />
        <h3 className="truncate text-[15px] font-medium text-text-primary">
          {cluster.name}
        </h3>
        <Tag className="ml-auto" variant={STATUS_TAG_VARIANT[cluster.status]}>
          {formatClusterStatus(cluster.status)}
        </Tag>
      </div>

      <MetricRow
        label="CPU"
        value={formatRatio(cluster.cpu.ratio)}
        ratio={cluster.cpu.ratio}
        threshold={threshold}
      />
      <MetricRow
        label="Mémoire"
        value={formatUsage(cluster.memory)}
        ratio={cluster.memory.ratio}
        threshold={threshold}
      />
      <MetricRow
        label="Stockage"
        value={formatUsage(cluster.storage)}
        ratio={cluster.storage.ratio}
        threshold={threshold}
      />

      <div className="mt-1 flex items-baseline justify-between py-[5px] text-[12px]">
        <span className="text-text-secondary">VM</span>
        <span className="text-text-primary">{vmSummary(cluster.vms)}</span>
      </div>

      {/*
        Every node, never a "N more nodes" tail: an operator scanning the
        overview needs to spot the one node that is down or in maintenance, and
        a truncated list hides exactly that. The card grows with the cluster —
        rows are one compact line each — and the grid row grows with it, which
        is cheaper than a nested scroller: a scrollable region inside a card
        that already carries role="button" would need its own tab stop, and a
        button may hold no focusable descendant.
      */}
      <ul>
        {cluster.nodes.map((node) => (
          <NodeRow key={node.name} node={node} />
        ))}
      </ul>

      {alert === undefined ? (
        <AlertBanner className="mt-2.5">{quietBanner(cluster)}</AlertBanner>
      ) : (
        <AlertBanner className="mt-2.5" icon={bannerIcon(alert)} variant="warning">
          {formatAlert(alert)}
        </AlertBanner>
      )}

      {freshness === null ? null : (
        <p className="mt-2 text-[11px] text-text-muted">{freshness}</p>
      )}
    </article>
  );
}
