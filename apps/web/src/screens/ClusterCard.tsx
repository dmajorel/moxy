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
  Thresholds,
  VmCounts,
} from "@/api/types";
import type {
  AlertBannerIcon,
  DataColumn,
  DataRow,
  SparklineTone,
  TagVariant,
} from "@/components/ui";
import {
  AlertBanner,
  ClusterAccent,
  DataTable,
  Sparkline,
  StatusDot,
  Tag,
  UsageBar,
} from "@/components/ui";
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
  formatUsageParts,
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
  /**
   * The ratios above which each reading turns amber, owned by the API payload.
   * One per resource: the storage bar is not coloured by the memory limit.
   */
  thresholds: Thresholds;
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

/** Banner sentence when nothing is wrong: the quorum, or nothing at all. */
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
  thresholds,
}: {
  cluster: ClusterOverview;
  usage: Series | null;
  thresholds: Thresholds;
}) {
  const points = usage?.points ?? [];
  const memory = formatUsageParts(cluster.memory);

  return (
    <div className="mb-1">
      <LegendRow
        label="CPU"
        value={formatRatio(cluster.cpu?.ratio ?? null)}
        detail={cpuCoresDetail(cluster.cpu)}
        tone="primary"
        warn={over(cluster.cpu?.ratio ?? null, thresholds.cpu)}
      />
      <LegendRow
        label="Mémoire"
        value={memory.value}
        detail={memory.detail}
        tone="secondary"
        warn={over(cluster.memory?.ratio ?? null, thresholds.memory)}
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
  /**
   * The quieter half of the figure, as in the legend rows above: the storage
   * row and the memory legend read alike on the same card, so the detail slot
   * cannot be one row's privilege.
   */
  detail?: string;
  /** null when the backend could not measure it: the bar stays empty. */
  ratio: number | null;
  threshold: number;
}

/** Label left, value right, fill bar underneath — the `.m` + `.bar` pair. */
function MetricRow({ label, value, detail, ratio, threshold }: MetricRowProps) {
  return (
    <div>
      <div className="flex items-baseline justify-between py-[5px] text-[12px]">
        <span className="text-text-secondary">{label}</span>
        <span className="text-text-primary">
          {value}
          {detail === undefined ? null : (
            <span className="ml-1 text-[11px] text-text-muted">{detail}</span>
          )}
        </span>
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

/**
 * The columns of the node list.
 *
 * The rows carried four values and named none of them: `4 % · 16 %` left the
 * reader to work out which number was the processor. What the list holds is a
 * table, so it is written as one — and `DataTable` is where a table's
 * accessibility lives in this interface: the `scope` of every header, the
 * caption naming it, one padding rule for every cell.
 *
 * The name column takes the spare width and the figures shrink to their own.
 * The reverse — a name capped at zero width, truncated to whatever was left —
 * cut names the card had room for: automatic layout serves every column its
 * share of the slack, so `En service`, whose heading is wider than any date it
 * ever holds, was paid for by the one column worth reading in full.
 *
 * A name too long for the card then wraps rather than being cut. The card
 * already grows with the cluster — no node is ever hidden — and growing by a
 * line is cheaper than the horizontal scroller a rigid table would need, which
 * sits under the title button's overlay where a pointer never reaches it.
 *
 * The dividers come with that width: once the name column is stretched, a short
 * name and its first figure are separated by an empty run, and the eye loses the
 * row the way it loses a line in a table of contents without leader dots.
 */
const NODE_COLUMNS: DataColumn[] = [
  { key: "node", header: "Nœud", fill: true },
  { key: "cpu", header: "CPU", align: "right", numeric: true, divider: true },
  { key: "memory", header: "Mémoire", align: "right", numeric: true, divider: true },
  // "Uptime" is the word everywhere but in the interface, which is French.
  {
    key: "uptime",
    header: "En service",
    align: "right",
    numeric: true,
    nowrap: true,
    divider: true,
    tone: "muted",
  },
];

/** Amber past the threshold, exactly as the cluster legend above already is. */
function metricCell(ratio: number | null, threshold: number) {
  const tone = over(ratio, threshold)
    ? "text-text-warning-strong"
    : "text-text-secondary";
  return <span className={tone}>{formatRatio(ratio)}</span>;
}

/**
 * One node as a row.
 *
 * The figures are the node's own. Those above the chart are the cluster's — a
 * CPU average weighted by cores, a sum of bytes — and an average is exactly
 * what hides the node worth looking at: a cluster at 28 % holding a node at
 * 95 % reads as quiet. The payload has carried the per-node readings all along.
 *
 * `null` stays the em dash, which `formatRatio` already writes: an offline
 * node, or one the token may not audit, is unknown and never idle. The header
 * of each column says what its figure is, so the numbers need no label of
 * their own.
 */
function nodeRow(node: Node, thresholds: Thresholds): DataRow {
  return {
    key: node.name,
    cells: {
      node: (
        <span className="flex items-center gap-1.5">
          <StatusDot status={node.status} />
          {/* The name is never cut. min-w-0 is what lets the flex child go
              below its content width at all, and break-words is the last
              resort for a hostname with nothing to break on — between them,
              the name wraps instead of widening the table, and no tooltip is
              needed to read what was lost, nothing being lost. */}
          <span className="min-w-0 break-words">{node.name}</span>
          {node.status === "maintenance" ? (
            <Tag className="shrink-0" variant="warning">
              {formatNodeStatus(node.status)}
            </Tag>
          ) : null}
        </span>
      ),
      cpu: metricCell(node.cpu?.ratio ?? null, thresholds.cpu),
      memory: metricCell(node.memory?.ratio ?? null, thresholds.memory),
      uptime: formatUptime(node.uptime),
    },
  };
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
  thresholds,
  onSelect,
  now,
  className,
}: ClusterCardProps) {
  const interactive = onSelect !== undefined;
  const freshness = freshnessLabel(cluster, now);
  const storage = formatUsageParts(cluster.storage);
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
      {/*
        No bottom margin: the banner below owns the single gap under the name.
        Two margins here would stack into twice the card's spacing unit.
      */}
      <div className="flex items-center gap-2">
        <StatusDot status={cluster.status} />
        {/*
          The accent of the configuration, when there is one: it names the
          cluster, so it sits against the name rather than against the status
          dot, and the gap-2 of the row is what keeps the two apart.
        */}
        <ClusterAccent color={cluster.color} className="-mr-0.5" />
        <h3
          id={titleId}
          // A long cluster name is cut by the card's width; the tooltip is the
          // only way to read the rest of it without opening the cluster.
          title={cluster.name}
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

      {/*
        Under the name, not at the foot of the card: a deliberate departure
        from appendix A.4, which draws the banner last.

        It is the only line of the card that says what there is to DO, and it
        was the last one read. No node is ever truncated, so the card grows
        with the cluster and on six nodes the banner fell below the fold: an
        operator sweeping the grid met reassuring figures first and the reason
        to stop only if they scrolled. Up here, the cluster's identity and its
        reason for attention are read in one go.

        The quiet banner moves with it, so the position never depends on the
        contents: on a grid where one card in three is in alert, an eye that
        has to hunt for the banner loses exactly what the move buys. The
        freshness line stays at the foot — it says how old the reading is, not
        what to do about it.

        EVERY alert, not just alerts[0].

        The header counts them all — "2 alertes" — and the card showed one, so
        an operator went looking for the second on another card and did not
        find it. Typically an available update hidden behind a memory warning,
        which appendix A.4 draws as a banner in its own right.

        Stacked rather than folded behind a "+1", for the same reason the node
        list below shows every node: the point of this screen is to spot what
        needs looking at, and anything a click away is something nobody clicked
        on. They are one compact line each and there are at most seven kinds.

        The wrapper carries both margins, so the gap above and below the banner
        is one rule each rather than two that add up.
      */}
      <div className="my-2.5">
        {cluster.alerts.length === 0 ? (
          <AlertBanner>{quietBanner(cluster)}</AlertBanner>
        ) : (
          cluster.alerts.map((entry, index) => (
            <AlertBanner
              key={`${entry.kind}:${String(index)}`}
              className={index === 0 ? undefined : "mt-1.5"}
              icon={bannerIcon(entry)}
              variant="warning"
            >
              {formatAlert(entry)}
            </AlertBanner>
          ))
        )}
      </div>

      <UsageChart cluster={cluster} usage={usage ?? null} thresholds={thresholds} />

      <MetricRow
        label="Stockage"
        value={storage.value}
        detail={storage.detail}
        ratio={cluster.storage.ratio}
        threshold={thresholds.storage}
      />

      <div className="mt-1 flex items-baseline justify-between py-[5px] text-[12px]">
        <span className="text-text-secondary">VM</span>
        <span className="text-text-primary">{vmSummary(cluster.vms)}</span>
      </div>

      {/*
        The heading stays above the table, whose own caption is for screen
        readers only: a heading is a place to jump to, and the sidebar names
        its sections the same way.
      */}
      <h4 className={NODE_HEADING_CLASSES}>Nœuds</h4>
      {/*
        Every node, never a "N more nodes" tail: an operator scanning the
        overview needs to spot the one node that is down or in maintenance, and
        a truncated list hides exactly that. The card grows with the cluster —
        rows are one compact line each — and the grid row grows with it, which
        is cheaper than a nested scroller: a scrollable region would need its
        own tab stop, on a card that already has exactly one.
      */}
      <DataTable
        caption={`Nœuds de ${cluster.name}`}
        columns={NODE_COLUMNS}
        rows={cluster.nodes.map((node) => nodeRow(node, thresholds))}
        emptyHint="Aucun nœud à afficher."
      />

      {freshness === null ? null : (
        <p className="mt-2 text-[11px] text-text-muted">{freshness}</p>
      )}
    </article>
  );
}
