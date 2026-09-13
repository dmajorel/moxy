/**
 * One cluster, summarised — the unit of screen 4 (appendix A.4 of the handoff).
 *
 * The card answers three questions without a click: is this cluster healthy,
 * how loaded is it, and what is the one thing worth looking at. Everything it
 * displays comes from `@/lib/format`; no unit, no percentage and no French
 * plural rule for a *value* is rebuilt here.
 */
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
  formatAlert,
  formatClusterStatus,
  formatCores,
  formatErrorKind,
  formatInteger,
  formatNodeStatus,
  formatRatio,
  formatRelativeTime,
  formatUptime,
  formatUsage,
  plural,
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
  /**
   * The instant the freshness line is measured against. Passed in rather than
   * read from the clock here so that one ticking clock serves the whole grid:
   * "il y a 12 s" used to be recomputed only at the next render, which during
   * an outage is exactly what stops happening — the age of the reading froze
   * at the moment it started mattering.
   */
  now?: Date;
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
    parts.push(`${formatInteger(vms.running)} en cours`);
  }
  if (vms.stopped > 0) {
    parts.push(plural(vms.stopped, "arrêtée", "arrêtées"));
  }
  if (vms.templates > 0) {
    parts.push(plural(vms.templates, "modèle", "modèles"));
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
function freshnessLabel(cluster: ClusterOverview, now?: Date): string | null {
  const relative =
    cluster.fetchedAt === null
      ? null
      : formatRelativeTime(new Date(cluster.fetchedAt), now ?? new Date());

  if (cluster.error !== null) {
    // The cause, not just the age. "Lecture ancienne · il y a 12 min" on a
    // cluster whose token was revoked sends an operator to look at the network.
    const cause = formatErrorKind(cluster.error);
    const suffix = cause === null ? "" : ` · ${cause}`;
    return relative === null
      ? `Aucune lecture disponible${suffix}`
      : `Lecture ancienne · ${relative}${suffix}`;
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
        {formatUptime(node.uptime)}
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

// `relative` is load-bearing: it is what the title button's overlay is
// positioned against. See TITLE_BUTTON_CLASSES.
const BASE_CLASSES = "relative rounded-panel bg-surface-2 px-4 py-3.5 text-left";

const INTERACTIVE_CLASSES = "cursor-pointer hover:bg-surface-1";

/**
 * The whole card is the button's hit area, and the button is the only thing in
 * it a keyboard or a screen reader can reach.
 *
 * The card used to BE the button — `role="button"` on the <article> — and that
 * is what made the overview screen inaudible. A button has "Children
 * Presentational: true" in WAI-ARIA: assistive technology announced "Cluster
 * Qualification, bouton" and nothing else, so the status, the memory at 83 %
 * and "Quorum perdu" all disappeared from the one screen that is always on
 * display.
 *
 * The fix cannot be a <button> around the card either: a button may hold
 * neither a heading nor a list, and this card holds both. So the button wraps
 * the NAME, and an ::after pseudo-element stretches its hit area over the
 * card. One tab stop, the whole content exposed, and the mouse affordance the
 * mockups draw is kept.
 *
 * The cost, stated plainly: the overlay swallows text selection inside the
 * card. Copying a node name means copying it from the node's own page. That is
 * the price of a card that is clickable everywhere AND readable by a screen
 * reader, and the second one is not optional.
 */
const TITLE_BUTTON_CLASSES =
  "truncate text-left after:absolute after:inset-0 after:rounded-panel " +
  "focus-visible:outline-none focus-visible:after:outline " +
  "focus-visible:after:outline-1 focus-visible:after:outline-offset-1 " +
  "focus-visible:after:outline-accent";

export function ClusterCard({
  cluster,
  usage,
  threshold,
  onSelect,
  now,
  className,
}: ClusterCardProps) {
  const interactive = onSelect !== undefined;
  const alert = cluster.alerts[0];
  const freshness = freshnessLabel(cluster, now);
  // Names the node list after its own visible heading, so the two cannot drift.
  const nodesHeadingId = useId();
  // The card is named by its own title rather than by an aria-label, so the
  // two cannot say different things.
  const titleId = useId();

  const classes = [
    BASE_CLASSES,
    STATUS_BORDER_CLASSES[cluster.status],
    interactive ? INTERACTIVE_CLASSES : "",
    className,
  ]
    .filter(Boolean)
    .join(" ");

  return (
    <article className={classes} aria-labelledby={titleId}>
      <div className="mb-2.5 flex items-center gap-2">
        <StatusDot status={cluster.status} />
        <h3
          id={titleId}
          className="truncate text-[15px] font-medium text-text-primary"
        >
          {onSelect === undefined ? (
            cluster.name
          ) : (
            <button
              type="button"
              // The visible label is the name; the accessible one says what
              // activating it does, and contains the visible text as WCAG
              // 2.5.3 requires.
              aria-label={`Ouvrir ${cluster.name}`}
              onClick={onSelect}
              className={TITLE_BUTTON_CLASSES}
            >
              {cluster.name}
            </button>
          )}
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
        is cheaper than a nested scroller: a scrollable region would need its
        own tab stop, on a card that already has exactly one.
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
