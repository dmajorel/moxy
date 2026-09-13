import { IconTool } from "@tabler/icons-react";

import type {
  Guest,
  NodeDetail as NodeDetailData,
  NodeUpdate,
  Series,
} from "@/api/types";
import type { DataTableColumn } from "@/components/ui";
import { ObjectHeader } from "@/components/ObjectHeader";
import {
  ChartCard,
  DataTable,
  KeyValue,
  MetricCard,
  StatusDot,
  Tag,
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
  formatUptime,
  formatUsage,
  formatVersionChange,
  plural,
} from "@/lib/format";

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
  threshold,
  onPlanMaintenance,
  onSelectGuest,
  className,
}: NodeDetailProps) {
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
        <ChartCard
          title="Charge CPU du nœud"
          label={`Charge CPU de ${node.name}`}
          series={series}
        />

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
          <DataTable
            caption="Invités hébergés par ce nœud"
            columns={guestColumns(onSelectGuest)}
            rows={guests}
            rowKey={(guest) => guest.vmid}
            rowClassName={() =>
              onSelectGuest === undefined ? undefined : "hover:bg-fill-ghost-selected"
            }
          />
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
            {/*
              No scroller of its own: the headings stick to the region above,
              and a second scroll container would be what they stuck to.
            */}
            <DataTable
              caption="Paquets en attente de mise à jour"
              columns={UPDATE_COLUMNS}
              rows={node.updates}
              rowKey={(update) => update.package}
              scrollable={false}
            />
          </div>
        </section>
      )}
    </div>
  );
}

/**
 * The guests of the node, one row each.
 *
 * A template reports neither load nor memory — it does not run — and the em
 * dash says so rather than a zero that would read as "idle".
 */
function guestColumns(
  onSelectGuest: ((vmid: number) => void) | undefined,
): DataTableColumn<Guest>[] {
  return [
    {
      header: "ID",
      cellClassName: "tabular-nums text-text-secondary",
      render: (guest) => guest.vmid,
    },
    {
      header: "Nom",
      cellClassName: "text-text-primary",
      render: (guest) => {
        const name = formatGuestName(guest.vmid, guest.name);
        if (onSelectGuest === undefined) {
          return name;
        }
        // The button carries the name alone, not the whole row: a <tr> is not
        // focusable, and the row would drag the figures into the accessible
        // name. The hover of the row is CSS, so the target still reads as a
        // line.
        return (
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
        );
      },
    },
    {
      header: "CPU",
      cellClassName: "tabular-nums text-text-secondary",
      render: (guest) =>
        guest.status === "template" ? FALLBACK : formatRatio(guest.cpu.ratio),
    },
    {
      header: "RAM",
      cellClassName: "tabular-nums text-text-secondary",
      render: (guest) =>
        guest.status === "template" ? FALLBACK : formatBytes(guest.memory.used),
    },
    {
      header: "État",
      render: (guest) =>
        guest.status === "template" ? (
          <Tag>{formatGuestStatus(guest.status)}</Tag>
        ) : (
          <Tag
            variant={guest.status === "running" ? "success" : "neutral"}
            icon={<StatusDot status={guest.status} decorative />}
          >
            {formatGuestStatus(guest.status)}
          </Tag>
        ),
    },
  ];
}

/** The headings stay visible while the package list scrolls under them. */
const STICKY_HEAD = "sticky top-0 bg-surface-2";

const UPDATE_COLUMNS: DataTableColumn<NodeUpdate>[] = [
  {
    header: "Paquet",
    headClassName: STICKY_HEAD,
    cellClassName: "font-mono text-text-primary",
    render: (update) => update.package,
  },
  {
    header: "Version",
    headClassName: STICKY_HEAD,
    cellClassName: "whitespace-nowrap font-mono text-text-secondary",
    render: (update) => formatVersionChange(update.oldVersion, update.version),
  },
  {
    header: "Description",
    headClassName: STICKY_HEAD,
    cellClassName: "text-text-muted",
    render: (update) => update.title ?? FALLBACK,
  },
];

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

