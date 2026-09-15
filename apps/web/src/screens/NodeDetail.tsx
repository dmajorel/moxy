import { IconTool } from "@tabler/icons-react";

import type {
  NodeDetail as NodeDetailData,
  Series,
  Thresholds,
  Timeframe,
} from "@/api/types";
import { ObjectHeader } from "@/components/ObjectHeader";
import type { DataColumn } from "@/components/ui";
import {
  ChartCard,
  DataTable,
  KeyValue,
  MetricCard,
  StatusDot,
  Tag,
} from "@/components/ui";
import { useFormat, useT } from "@/i18n/locale";
import type { Translator } from "@/i18n/messages";
import type { Format } from "@/lib/format";
import { FALLBACK, formatGuestName, formatVersionChange } from "@/lib/format";

/**
 * Node view — screen 2 of the mockups.
 *
 * No tab bar: the handoff draws six tabs, but only the summary has anything
 * behind it today, and five dead tabs would promise what does not exist.
 */
export interface NodeDetailProps {
  node: NodeDetailData;
  clusterName: string;
  series: Series | null;
  /** The window asked for, which the picker shows as the current choice. */
  timeframe: Timeframe;
  /**
   * Picks another window. Omitted, the chart keeps the one it is given and no
   * picker is drawn — a radio group nobody listens to would be a dead control.
   */
  onTimeframeChange?: (timeframe: Timeframe) => void;
  /** One per resource: the local disk is not coloured by the memory limit. */
  thresholds: Thresholds;
  /** Opens the drain plan. Omitted, the button is not rendered at all. */
  onPlanMaintenance?: () => void;
  /**
   * Opens a guest of this node. Omitted, the names stay plain text: a button
   * that leads nowhere would promise a navigation the caller cannot perform.
   */
  onSelectGuest?: (vmid: number) => void;
  className?: string;
}

function guestColumns(t: Translator): DataColumn[] {
  return [
    { key: "vmid", header: t("node.column.vmid"), numeric: true, tone: "secondary" },
    { key: "name", header: t("node.column.name") },
    { key: "cpu", header: t("node.column.cpu"), numeric: true, tone: "secondary" },
    { key: "memory", header: t("node.column.memory"), numeric: true, tone: "secondary" },
    { key: "status", header: t("node.column.status") },
  ];
}

function updateColumns(t: Translator): DataColumn[] {
  return [
    { key: "package", header: t("node.updates.column.package"), mono: true },
    {
      key: "version",
      header: t("node.updates.column.version"),
      mono: true,
      nowrap: true,
      tone: "secondary",
    },
    { key: "title", header: t("node.updates.column.title"), tone: "muted" },
  ];
}

export function NodeDetail({
  node,
  clusterName,
  series,
  timeframe,
  onTimeframeChange,
  thresholds,
  onPlanMaintenance,
  onSelectGuest,
  className,
}: NodeDetailProps) {
  const t = useT();
  const fmt = useFormat();
  const guests = node.guests;
  const templates = guests.filter((guest) => guest.status === "template").length;
  const running = guests.length - templates;

  const chips = [
    node.pveVersion === null ? null : `PVE ${node.pveVersion}`,
    // The counts come from the same helper as every other one in the
    // interface, so the plural rule is decided in one place.
    templates === 0
      ? fmt.plural(running, "guest")
      : `${fmt.plural(running, "guest")} · ${fmt.plural(templates, "template")}`,
  ].filter((chip): chip is string => chip !== null);

  const memory = fmt.formatUsageParts(node.memory);
  const rootfs = fmt.formatUsageParts(node.rootfs);

  return (
    <div className={className}>
      <ObjectHeader
        breadcrumb={[clusterName, t("node.breadcrumb")]}
        name={node.name}
        status={node.status}
        stateLabel={nodeStateLabel(node, fmt)}
        chips={chips}
        actions={
          onPlanMaintenance === undefined ? undefined : (
            <button
              type="button"
              onClick={onPlanMaintenance}
              className="flex items-center gap-1.5 rounded-card border-[0.5px] border-warning bg-bg-warning px-2.5 py-1.5 text-[12px] text-text-warning hover:brightness-95"
            >
              <IconTool size={14} aria-hidden />
              {t("node.planMaintenance")}
            </button>
          )
        }
      />

      <div className="mb-3.5 grid grid-cols-1 gap-2.5 sm:grid-cols-2 xl:grid-cols-4">
        <MetricCard
          label={t("node.metric.cpu")}
          value={fmt.formatRatio(node.cpu.ratio)}
          detail={`· ${fmt.formatCores(node.cpu.cores)}`}
          ratio={node.cpu.ratio}
          threshold={thresholds.cpu}
        />
        <MetricCard
          label={t("node.metric.memory")}
          value={memory.value}
          detail={memory.detail}
          ratio={node.memory.ratio}
          threshold={thresholds.memory}
        />
        <MetricCard
          label={t("node.metric.localStorage")}
          value={rootfs.value}
          detail={rootfs.detail}
          ratio={node.rootfs.ratio}
          threshold={thresholds.storage}
        />
        <MetricCard
          label={t("node.metric.loadAverage")}
          value={fmt.formatLoadAverage(node.loadAverage)}
          detail={
            node.loadAverage === null ? undefined : t("node.loadAverageDetail")
          }
        />
      </div>

      <div className="mb-3.5 grid grid-cols-1 gap-2.5 lg:grid-cols-[minmax(0,1.5fr)_minmax(0,1fr)]">
        <ChartCard
          title={t("node.chartTitle")}
          label={t("node.chartLabel", { name: node.name })}
          series={series}
          timeframe={timeframe}
          onTimeframeChange={onTimeframeChange}
        />

        <section className="rounded-card border-[0.5px] border-border bg-surface-2 px-3 py-1">
          <KeyValue
            rows={[
              { label: t("node.kv.cluster"), value: clusterName },
              { label: t("node.kv.quorum"), value: fmt.formatQuorum(node.quorum) },
              { label: t("node.kv.ha"), value: fmt.formatHaState(node.haState) },
              { label: t("node.kv.kernel"), value: node.kernelVersion, mono: true },
              {
                label: t("node.kv.updates"),
                value: fmt.formatPendingUpdates(node.pendingUpdates),
              },
            ]}
          />
        </section>
      </div>

      <section className="rounded-card border-[0.5px] border-border bg-surface-2 px-3 py-2.5">
        <div className="mb-1.5 flex flex-wrap items-center gap-3">
          <h2 className="text-[12px] font-medium text-text-primary">
            {t("node.guests.title")}
          </h2>
          {node.status === "maintenance" && (
            <span className="text-[11px] text-text-warning-strong">
              {t("node.guests.drained")}
            </span>
          )}
        </div>

        <DataTable
          caption={t("node.guests.caption")}
          columns={guestColumns(t)}
          emptyHint={
            node.status === "maintenance"
              ? t("node.guests.emptyDrained")
              : t("node.guests.empty")
          }
          rows={guests.map((guest) => {
            const name = formatGuestName(guest.vmid, guest.name);
            return {
              key: String(guest.vmid),
              className:
                onSelectGuest === undefined ? undefined : "hover:bg-fill-ghost-selected",
              cells: {
                vmid: guest.vmid,
                /*
                 * The button carries the name alone, not the whole row: a <tr>
                 * is not focusable, and the row would drag the figures into
                 * the accessible name. The hover of the row is CSS, so the
                 * target still reads as a line.
                 */
                name:
                  onSelectGuest === undefined ? (
                    name
                  ) : (
                    <button
                      type="button"
                      aria-label={t("node.guests.open", { name })}
                      onClick={() => {
                        onSelectGuest(guest.vmid);
                      }}
                      className="rounded-card text-left hover:underline focus:outline-none focus-visible:outline-1 focus-visible:outline-accent"
                    >
                      {name}
                    </button>
                  ),
                // A template is not running, so it has no reading to show:
                // the dash says "nothing to measure", not "zero".
                cpu:
                  guest.status === "template"
                    ? FALLBACK
                    : fmt.formatRatio(guest.cpu.ratio),
                memory:
                  guest.status === "template"
                    ? FALLBACK
                    : fmt.formatBytes(guest.memory.used),
                status:
                  guest.status === "template" ? (
                    <Tag>{fmt.formatGuestStatus(guest.status)}</Tag>
                  ) : (
                    <Tag
                      variant={guest.status === "running" ? "success" : "neutral"}
                      icon={<StatusDot status={guest.status} decorative />}
                    >
                      {fmt.formatGuestStatus(guest.status)}
                    </Tag>
                  ),
              },
            };
          })}
        />
      </section>

      {/*
       * Only rendered when something is actually pending. "À jour" and the dash
       * of an unaskable node are already said by the row above, and an empty
       * table would say them a second time, worse.
       */}
      {node.updates !== null && node.updates.length > 0 && (
        <section className="mt-3.5 rounded-card border-[0.5px] border-border bg-surface-2 px-3 py-2.5">
          <div className="mb-1.5 flex flex-wrap items-center gap-3">
            <h2 className="text-[12px] font-medium text-text-primary">
              {t("node.updates.title")}
            </h2>
            <span className="text-[11px] text-text-muted">
              {fmt.formatPackageCount(node.updates.length)}
            </span>
          </div>
          {/*
            A scrollable region must be focusable, which is WCAG 2.1.1: a
            keyboard user otherwise sees the top of the list and nothing else.
            tabIndex makes it a scroll container the arrows work in, and the
            label names a group that is otherwise anonymous. The lint rule
            reads "tabindex on a group" and cannot know the element scrolls.
          */}
          {/* eslint-disable jsx-a11y/no-noninteractive-tabindex -- scrollable region */}
          <div
            tabIndex={0}
            role="group"
            aria-label={t("node.updates.group")}
            className="max-h-72 overflow-auto focus-visible:outline focus-visible:outline-1 focus-visible:outline-offset-1 focus-visible:outline-accent"
          >
            {/* eslint-enable jsx-a11y/no-noninteractive-tabindex */}
            <DataTable
              caption={t("node.updates.caption")}
              columns={updateColumns(t)}
              // Unreachable: the section is only rendered when the list has
              // something in it. Stated all the same, since DataTable requires
              // one and a table with no fallback is a blank panel.
              emptyHint={t("node.updates.empty")}
              stickyHeader
              rows={node.updates.map((update) => ({
                key: update.package,
                cells: {
                  package: update.package,
                  version: formatVersionChange(update.oldVersion, update.version),
                  title: update.title ?? FALLBACK,
                },
              }))}
            />
          </div>
        </section>
      )}
    </div>
  );
}

/**
 * The line under the node name: its state, and how long it has been in it.
 *
 * A node with no uptime to report gets the state alone. "Hors ligne · —"
 * spends the line on saying nothing twice, and the state is what the §2 asks
 * to be read first.
 */
function nodeStateLabel(node: NodeDetailData, fmt: Format): string {
  const status = fmt.formatNodeStatus(node.status);
  return node.uptime === null
    ? status
    : `${status} · ${fmt.formatUptime(node.uptime)}`;
}

