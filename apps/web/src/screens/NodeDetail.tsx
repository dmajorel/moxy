import { IconTool } from "@tabler/icons-react";

import type { NodeDetail as NodeDetailData, Series, Timeframe } from "@/api/types";
import { ObjectHeader } from "@/components/ObjectHeader";
import {
  KeyValue,
  MetricCard,
  Sparkline,
  StatusDot,
  Tag,
  TimeframePicker,
} from "@/components/ui";
import {
  FALLBACK,
  formatBytes,
  formatCores,
  formatGuestName,
  formatGuestStatus,
  formatHaState,
  formatLoadAverage,
  formatNodeStatus,
  formatPackageCount,
  formatPendingUpdates,
  formatQuorum,
  formatRatio,
  formatTimeframe,
  formatUptime,
  formatUsage,
  formatVersionChange,
  plural,
} from "@/lib/format";
import { cpuRatios, timeTicks } from "@/lib/series";

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
  threshold: number;
  /** Opens the drain plan. Omitted, the button is not rendered at all. */
  onPlanMaintenance?: () => void;
  /**
   * Opens a guest of this node. Omitted, the names stay plain text: a button
   * that leads nowhere would promise a navigation the caller cannot perform.
   */
  onSelectGuest?: (vmid: number) => void;
  className?: string;
}

export function NodeDetail({
  node,
  clusterName,
  series,
  timeframe,
  onTimeframeChange,
  threshold,
  onPlanMaintenance,
  onSelectGuest,
  className,
}: NodeDetailProps) {
  // The window the payload says it holds, not the one last asked for: a
  // reading that arrives after a switch is labelled with its own span rather
  // than with the button that is now lit.
  const shownTimeframe = series?.timeframe ?? timeframe;
  const guests = node.guests;
  const templates = guests.filter((guest) => guest.status === "template").length;
  const running = guests.length - templates;

  const chips = [
    node.pveVersion === null ? null : `PVE ${node.pveVersion}`,
    // "invité" and "modèle" both take the plural; the counts come from the
    // same helper as every other one in the interface.
    templates === 0
      ? plural(running, "invité", "invités")
      : `${plural(running, "invité", "invités")} · ${plural(templates, "modèle", "modèles")}`,
  ].filter((chip): chip is string => chip !== null);

  return (
    <div className={className}>
      <ObjectHeader
        breadcrumb={[clusterName, "Nœud"]}
        name={node.name}
        status={node.status}
        stateLabel={nodeStateLabel(node)}
        chips={chips}
        actions={
          onPlanMaintenance === undefined ? undefined : (
            <button
              type="button"
              onClick={onPlanMaintenance}
              className="flex items-center gap-1.5 rounded-card border-[0.5px] border-warning bg-bg-warning px-2.5 py-1.5 text-[12px] text-text-warning hover:brightness-95"
            >
              <IconTool size={14} aria-hidden />
              Plan de maintenance
            </button>
          )
        }
      />

      <div className="mb-3.5 grid grid-cols-1 gap-2.5 sm:grid-cols-2 xl:grid-cols-4">
        <MetricCard
          label="CPU"
          value={formatRatio(node.cpu.ratio)}
          detail={`· ${formatCores(node.cpu.cores)}`}
          ratio={node.cpu.ratio}
          threshold={threshold}
        />
        <MetricCard
          label="Mémoire"
          value={formatUsage(node.memory)}
          ratio={node.memory.ratio}
          threshold={threshold}
        />
        <MetricCard
          label="Stockage local"
          value={formatUsage(node.rootfs)}
          ratio={node.rootfs.ratio}
          threshold={threshold}
        />
        <MetricCard
          label="Load average"
          value={formatLoadAverage(node.loadAverage)}
          detail={node.loadAverage === null ? undefined : "· 1, 5, 15 min"}
        />
      </div>

      <div className="mb-3.5 grid grid-cols-1 gap-2.5 lg:grid-cols-[minmax(0,1.5fr)_minmax(0,1fr)]">
        <section className="rounded-card border-[0.5px] border-border bg-surface-2 px-3 py-2.5">
          <div className="mb-1.5 flex flex-wrap items-center justify-between gap-x-3 gap-y-1">
            <div className="flex items-center gap-2">
              <h2 className="text-[12px] font-medium text-text-primary">
                Charge CPU du nœud
              </h2>
              {onTimeframeChange !== undefined && (
                <TimeframePicker
                  value={timeframe}
                  onChange={onTimeframeChange}
                  label="Fenêtre du graphe"
                />
              )}
            </div>
            <span className="text-[11px] text-text-muted">
              {series === null
                ? formatTimeframe(shownTimeframe)
                : `${formatTimeframe(shownTimeframe)} · moy. ${formatRatio(series.cpuAverage)}`}
            </span>
          </div>
          <Sparkline
            series={[{ values: cpuRatios(series?.points ?? []) }]}
            label={`Charge CPU de ${node.name}`}
            // The "11:00 · 11:30 · 12:00" of appendix A.1: a chart with no
            // time axis does not say when the spike it shows happened. Past
            // the day the marks carry a date instead — an hour tells nothing
            // about where a sample sits in a month.
            ticks={timeTicks(series?.points ?? [], shownTimeframe)}
          />
        </section>

        <section className="rounded-card border-[0.5px] border-border bg-surface-2 px-3 py-1">
          <KeyValue
            rows={[
              { label: "Cluster", value: clusterName },
              { label: "Quorum", value: formatQuorum(node.quorum) },
              { label: "HA", value: formatHaState(node.haState) },
              { label: "Noyau", value: node.kernelVersion, mono: true },
              {
                label: "Mises à jour",
                value: formatPendingUpdates(node.pendingUpdates),
              },
            ]}
          />
        </section>
      </div>

      <section className="rounded-card border-[0.5px] border-border bg-surface-2 px-3 py-2.5">
        <div className="mb-1.5 flex flex-wrap items-center gap-3">
          <h2 className="text-[12px] font-medium text-text-primary">
            Invités sur ce nœud
          </h2>
          {node.status === "maintenance" && (
            <span className="text-[11px] text-text-warning-strong">
              Nœud en maintenance : les invités gérés par HA ont été migrés.
            </span>
          )}
        </div>

        {guests.length === 0 ? (
          <p className="text-[12px] text-text-muted">
            {node.status === "maintenance"
              ? "Ce nœud a été vidé par la mise en maintenance."
              : "Aucun invité sur ce nœud."}
          </p>
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full border-collapse text-[12px]">
              <caption className="sr-only">Invités hébergés par ce nœud</caption>
              <thead>
                <tr className="text-left text-[11px] text-text-muted">
                  <th scope="col" className="py-1.5 pr-3 font-normal">ID</th>
                  <th scope="col" className="py-1.5 pr-3 font-normal">Nom</th>
                  <th scope="col" className="py-1.5 pr-3 font-normal">CPU</th>
                  <th scope="col" className="py-1.5 pr-3 font-normal">RAM</th>
                  <th scope="col" className="py-1.5 font-normal">État</th>
                </tr>
              </thead>
              <tbody>
                {guests.map((guest) => {
                  const name = formatGuestName(guest.vmid, guest.name);
                  return (
                    <tr
                      key={guest.vmid}
                      className={
                        "border-t-[0.5px] border-border" +
                        (onSelectGuest === undefined
                          ? ""
                          : " hover:bg-fill-ghost-selected")
                      }
                    >
                      <td className="py-1.5 pr-3 tabular-nums text-text-secondary">
                        {guest.vmid}
                      </td>
                      <td className="py-1.5 pr-3 text-text-primary">
                        {/*
                         * The button carries the name alone, not the whole row:
                         * a <tr> is not focusable, and the row would drag the
                         * figures into the accessible name. The hover of the
                         * row is CSS, so the target still reads as a line.
                         */}
                        {onSelectGuest === undefined ? (
                          name
                        ) : (
                          <button
                            type="button"
                            aria-label={`Ouvrir ${name}`}
                            onClick={() => {
                              onSelectGuest(guest.vmid);
                            }}
                            className="rounded-card text-left hover:underline focus:outline-none focus-visible:outline-1 focus-visible:outline-accent"
                          >
                            {name}
                          </button>
                        )}
                      </td>
                      <td className="py-1.5 pr-3 tabular-nums text-text-secondary">
                        {guest.status === "template"
                          ? FALLBACK
                          : formatRatio(guest.cpu.ratio)}
                      </td>
                      <td className="py-1.5 pr-3 tabular-nums text-text-secondary">
                        {guest.status === "template"
                          ? FALLBACK
                          : formatBytes(guest.memory.used)}
                      </td>
                      <td className="py-1.5">
                        {guest.status === "template" ? (
                          <Tag>{formatGuestStatus(guest.status)}</Tag>
                        ) : (
                          <Tag
                            variant={guest.status === "running" ? "success" : "neutral"}
                            icon={<StatusDot status={guest.status} decorative />}
                          >
                            {formatGuestStatus(guest.status)}
                          </Tag>
                        )}
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        )}
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
              Mises à jour en attente
            </h2>
            <span className="text-[11px] text-text-muted">
              {formatPackageCount(node.updates.length)}
            </span>
          </div>
          {/*
            A scrollable region that the keyboard cannot reach is a list a
            keyboard user can see the top of and nothing else. tabIndex makes
            it a scroll container the arrows work in, and the label says what
            it holds, since the group is otherwise anonymous.
          */}
          {/*
            A scrollable region must be focusable, which is WCAG 2.1.1: a
            keyboard user otherwise sees the top of the list and nothing else.
            The rule reads "tabindex on a group" and cannot know the element
            scrolls.
          */}
          {/* eslint-disable jsx-a11y/no-noninteractive-tabindex -- scrollable region */}
          <div
            tabIndex={0}
            role="group"
            aria-label="Paquets en attente"
            className="max-h-72 overflow-auto focus-visible:outline focus-visible:outline-1 focus-visible:outline-offset-1 focus-visible:outline-accent"
          >
          {/* eslint-enable jsx-a11y/no-noninteractive-tabindex */}
            <table className="w-full border-collapse text-[12px]">
              <caption className="sr-only">Paquets en attente de mise à jour</caption>
              <thead>
                <tr className="text-left text-[11px] text-text-muted">
                  <th scope="col" className="sticky top-0 bg-surface-2 py-1.5 pr-3 font-normal">
                    Paquet
                  </th>
                  <th scope="col" className="sticky top-0 bg-surface-2 py-1.5 pr-3 font-normal">
                    Version
                  </th>
                  <th scope="col" className="sticky top-0 bg-surface-2 py-1.5 font-normal">
                    Description
                  </th>
                </tr>
              </thead>
              <tbody>
                {node.updates.map((update) => (
                  <tr
                    key={update.package}
                    className="border-t-[0.5px] border-border"
                  >
                    <td className="py-1.5 pr-3 font-mono text-text-primary">
                      {update.package}
                    </td>
                    <td className="whitespace-nowrap py-1.5 pr-3 font-mono text-text-secondary">
                      {formatVersionChange(update.oldVersion, update.version)}
                    </td>
                    <td className="py-1.5 text-text-muted">
                      {update.title ?? FALLBACK}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
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
function nodeStateLabel(node: NodeDetailData): string {
  const status = formatNodeStatus(node.status);
  return node.uptime === null ? status : `${status} · ${formatUptime(node.uptime)}`;
}

