/**
 * One cluster, summarised — the unit of screen 4 (appendix A.4 of the handoff).
 *
 * The card answers three questions without a click: is this cluster healthy,
 * how loaded is it, and what is the one thing worth looking at. Everything it
 * displays comes from `@/lib/format`; no unit, no percentage and no French
 * plural rule for a *value* is rebuilt here.
 */
import type { KeyboardEvent } from "react";
import { useId } from "react";

import type {
  Alert,
  ClusterOverview,
  ClusterStatus,
  Cpu,
  Node,
  Series,
  VmCounts,
} from "@/api/types";
import type { AlertBannerIcon, SparklineTone, TagVariant } from "@/components/ui";
import { AlertBanner, Sparkline, StatusDot, Tag, UsageBar } from "@/components/ui";
import {
  FALLBACK,
  formatAlert,
  formatClusterStatus,
  formatCores,
  formatNodeStatus,
  formatRatio,
  formatRelativeTime,
  formatUptime,
  formatUsage,
} from "@/lib/format";
import { cpuRatios, memoryRatios } from "@/lib/series";

export interface ClusterCardProps {
  cluster: ClusterOverview;
  /**
   * The last hour of the cluster, or null while it is being fetched or could
   * not be. The card keeps its figures either way: a chart that cannot be
   * drawn costs the curve, never the card.
   */
  usage?: Series | null;
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

/**
 * Height of the card chart.
 *
 * The 70 px of section 2 are for the detail screens, where the chart is the
 * subject. Here it sits between the figures and the node list, and has to say
 * the shape of the hour without pushing the nodes below the fold.
 */
const CHART_HEIGHT = 48;

/** The colour of each curve, as the swatch its legend line carries. */
const SWATCH_CLASSES: Record<SparklineTone, string> = {
  primary: "bg-accent",
  secondary: "bg-text-muted",
};

interface LegendRowProps {
  label: string;
  value: string;
  /**
   * What the value is measured against, written quieter next to it, as in
   * `31 % · 96 c`. Left out when there is nothing to say: an unmeasured
   * metric already reads as the em dash and needs no second one.
   */
  detail?: string;
  tone: SparklineTone;
  /** Past the threshold the figure itself turns amber, as the bar used to. */
  warn?: boolean;
}

/**
 * One metric of the chart: its colour, its name and its current figure.
 *
 * The swatch is decorative — the curve is named in words right beside it, so
 * nothing is carried by colour alone — and the value is the instantaneous
 * reading the gauges used to show, which the curve does not replace: an hour
 * says where the cluster is heading, not where it is.
 */
function LegendRow({ label, value, detail, tone, warn = false }: LegendRowProps) {
  return (
    <div className="flex items-baseline justify-between py-[5px] text-[12px]">
      <span className="flex items-center gap-1.5 text-text-secondary">
        <span
          aria-hidden
          className={`inline-block h-[2px] w-3 rounded-full ${SWATCH_CLASSES[tone]}`}
        />
        {label}
      </span>
      <span className={warn ? "text-text-warning-strong" : "text-text-primary"}>
        {value}
        {detail === undefined ? null : (
          <span className="ml-1 text-[11px] text-text-muted">{detail}</span>
        )}
      </span>
    </div>
  );
}

/**
 * The last hour of the cluster, in place of the CPU and memory gauges.
 *
 * A gauge only ever says "now", and on the screen an operator leaves open the
 * question is rather whether anything is drifting: 70 % on the way up and 70 %
 * on the way down ask for different things and a bar draws them alike.
 *
 * The scale stays pinned to [0,1] — never derived from the points — so that
 * two cards side by side are comparable and a quiet cluster looks quiet, which
 * is the correction section 2 makes to the native interface.
 */
function UsageChart({
  cluster,
  usage,
  threshold,
}: {
  cluster: ClusterOverview;
  usage: Series | null;
  threshold: number;
}) {
  const points = usage?.points ?? [];

  return (
    <div className="mb-1">
      <LegendRow
        label="CPU"
        value={formatRatio(cluster.cpu?.ratio ?? null)}
        detail={cpuCoresDetail(cluster.cpu)}
        tone="primary"
        warn={over(cluster.cpu?.ratio ?? null, threshold)}
      />
      <LegendRow
        label="Mémoire"
        value={formatUsage(cluster.memory)}
        tone="secondary"
        warn={over(cluster.memory?.ratio ?? null, threshold)}
      />
      <Sparkline
        className="mt-[2px]"
        height={CHART_HEIGHT}
        label={chartLabel(cluster)}
        series={[
          { values: cpuRatios(points), tone: "primary" },
          { values: memoryRatios(points), tone: "secondary" },
        ]}
      />
      <p className="mt-[2px] text-right text-[11px] text-text-muted">Dernière heure</p>
    </div>
  );
}

/**
 * Whether a ratio has passed the threshold the API set.
 *
 * Unknown never warns: a metric nobody could measure is not a metric that is
 * high. The amber moved from the bar to the figure when the bars gave way to
 * the chart — a curve pinned to [0,1] cannot carry it, and the card would
 * otherwise have lost the one cue that says "look here".
 */
function over(ratio: number | null, threshold: number): boolean {
  return ratio !== null && Number.isFinite(ratio) && ratio > threshold;
}

/** Says in words what the two curves show, for whoever cannot see them. */
function chartLabel(cluster: ClusterOverview): string {
  const cpu = formatRatio(cluster.cpu?.ratio ?? null);
  const memory = formatRatio(cluster.memory?.ratio ?? null);
  return `Utilisation de ${cluster.name} sur la dernière heure : CPU ${cpu}, mémoire ${memory}`;
}

interface MetricRowProps {
  label: string;
  value: string;
  /** null when the backend could not measure it: the bar stays empty. */
  ratio: number | null;
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
        ratio={ratio ?? Number.NaN}
        size={4}
        threshold={threshold}
        label={label}
      />
    </div>
  );
}

const NODE_ROW_CLASSES =
  "flex items-center gap-1.5 border-t-[0.5px] border-border py-[5px] text-[12px]";

/**
 * The section label above the node list.
 *
 * Without it the rows sit straight under the "VM" line and read as its detail —
 * a list of VMs rather than a list of nodes. The treatment is the one section 2
 * prescribes and the handoff's own sidebar uses for "Nœuds" and "Machines
 * virtuelles": 11px muted type, hierarchy carried by typography. The hairline
 * already topping every node row then falls under the label and rules it off,
 * so no extra border and no new colour are introduced.
 */
const NODE_HEADING_CLASSES = "mt-2 mb-[2px] text-[11px] text-text-muted";

function NodeRow({ node }: { node: Node }) {
  return (
    <li className={NODE_ROW_CLASSES}>
      <StatusDot status={node.status} />
      <span className="truncate text-text-primary">{node.name}</span>
      <span className="ml-auto shrink-0 tabular-nums text-[11px] text-text-muted">
        {nodeUptime(node)}
      </span>
      {node.status === "maintenance" ? (
        <Tag className="shrink-0" variant="warning">
          {formatNodeStatus(node.status)}
        </Tag>
      ) : null}
    </li>
  );
}

/**
 * Uptime of a node, or the em dash when the figure would be a lie.
 *
 * PVE reports uptime 0 for a node it cannot reach, and rendering that as "0 s"
 * would claim the node had just booted. A node in maintenance, on the other
 * hand, is still up: it refuses new guests, it did not restart.
 */
function nodeUptime(node: Node): string {
  if (node.status === "offline" || node.status === "unknown" || node.uptime <= 0) {
    return FALLBACK;
  }
  return formatUptime(node.uptime);
}

/**
 * The processor count a cluster's CPU load is a fraction of, or nothing.
 *
 * It is the sum over the nodes that are up *and* reported figures, so a
 * cluster PVE would not let moxy audit shows a smaller total than it owns —
 * the `node_stats_unavailable` banner on the same card already says why, and
 * a second warning here would only crowd the line. When no node reported at
 * all, the ratio itself is already the em dash: adding `— · —` says the same
 * thing twice.
 */
function cpuCoresDetail(cpu: Cpu | null): string | undefined {
  if (cpu === null || cpu.cores <= 0) {
    return undefined;
  }
  return `· ${formatCores(cpu.cores)}`;
}

const BASE_CLASSES = "rounded-panel bg-surface-2 px-4 py-3.5 text-left";

const INTERACTIVE_CLASSES =
  "cursor-pointer hover:bg-surface-1 focus-visible:outline " +
  "focus-visible:outline-1 focus-visible:outline-offset-1 " +
  "focus-visible:outline-accent";

export function ClusterCard({
  cluster,
  usage,
  threshold,
  onSelect,
  className,
}: ClusterCardProps) {
  const interactive = onSelect !== undefined;
  const alert = cluster.alerts[0];
  const freshness = freshnessLabel(cluster);
  // Names the node list after its own visible heading, so the two cannot drift.
  const nodesHeadingId = useId();

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

      <UsageChart cluster={cluster} usage={usage ?? null} threshold={threshold} />

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

      <h4 className={NODE_HEADING_CLASSES} id={nodesHeadingId}>
        Nœuds
      </h4>
      {/*
        Every node, never a "N more nodes" tail: an operator scanning the
        overview needs to spot the one node that is down or in maintenance, and
        a truncated list hides exactly that. The card grows with the cluster —
        rows are one compact line each — and the grid row grows with it, which
        is cheaper than a nested scroller: a scrollable region inside a card
        that already carries role="button" would need its own tab stop, and a
        button may hold no focusable descendant.
      */}
      <ul aria-labelledby={nodesHeadingId}>
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
