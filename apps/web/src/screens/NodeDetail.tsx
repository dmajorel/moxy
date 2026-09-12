import { IconTool } from "@tabler/icons-react";

import type { NodeDetail as NodeDetailData, Series } from "@/api/types";
import { ObjectHeader } from "@/components/ObjectHeader";
import { KeyValue, MetricCard, Sparkline, StatusDot, Tag } from "@/components/ui";
import {
  formatBytes,
  formatNodeStatus,
  formatRatio,
  formatUptime,
  formatUsage,
  truncateGuestLabel,
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
  className?: string;
}

export function NodeDetail({
  node,
  clusterName,
  series,
  threshold,
  onPlanMaintenance,
  className,
}: NodeDetailProps) {
  const guests = node.guests;
  const templates = guests.filter((guest) => guest.status === "template").length;
  const running = guests.length - templates;

  const chips = [
    node.pveVersion === null ? null : `PVE ${node.pveVersion}`,
    // "VM" is invariable in French; only "template" takes the plural.
    templates === 0
      ? `${String(running)} VM`
      : `${String(running)} VM · ${String(templates)} template${templates > 1 ? "s" : ""}`,
  ].filter((chip): chip is string => chip !== null);

  return (
    <div className={className}>
      <ObjectHeader
        breadcrumb={[clusterName, "Nœud"]}
        name={node.name}
        status={node.status}
        stateLabel={`${formatNodeStatus(node.status)} · ${formatUptime(node.uptime)}`}
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
          detail={`· ${String(node.cpu.cores)} c`}
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
          value={formatLoad(node.loadAverage)}
          detail={node.loadAverage === null ? undefined : "· 1, 5, 15 min"}
        />
      </div>

      <div className="mb-3.5 grid grid-cols-1 gap-2.5 lg:grid-cols-[minmax(0,1.5fr)_minmax(0,1fr)]">
        <section className="rounded-card border-[0.5px] border-border bg-surface-2 px-3 py-2.5">
          <div className="mb-1.5 flex items-center justify-between gap-3">
            <h2 className="text-[12px] font-medium text-text-primary">
              Charge CPU du nœud
            </h2>
            <span className="text-[11px] text-text-muted">
              {series === null
                ? "Dernière heure"
                : `Dernière heure · moy. ${formatRatio(series.cpuAverage)}`}
            </span>
          </div>
          <Sparkline
            points={series?.points ?? []}
            label={`Charge CPU de ${node.name}`}
          />
        </section>

        <section className="rounded-card border-[0.5px] border-border bg-surface-2 px-3 py-1">
          <KeyValue
            rows={[
              { label: "Cluster", value: clusterName },
              { label: "Quorum", value: formatQuorum(node) },
              { label: "HA", value: node.haState },
              { label: "Noyau", value: node.kernelVersion, mono: true },
              {
                label: "Mises à jour",
                value:
                  node.pendingUpdates === null
                    ? null
                    : node.pendingUpdates === 0
                      ? "À jour"
                      : `${String(node.pendingUpdates)} en attente`,
              },
            ]}
          />
        </section>
      </div>

      <section className="rounded-card border-[0.5px] border-border bg-surface-2 px-3 py-2.5">
        <div className="mb-1.5 flex flex-wrap items-center gap-3">
          <h2 className="text-[12px] font-medium text-text-primary">
            Machines virtuelles sur ce nœud
          </h2>
          {node.status === "maintenance" && (
            <span className="text-[11px] text-text-warning-strong">
              Nœud en maintenance : les VM gérées par HA ont été migrées.
            </span>
          )}
        </div>

        {guests.length === 0 ? (
          <p className="text-[12px] text-text-muted">
            {node.status === "maintenance"
              ? "Ce nœud a été vidé par la mise en maintenance."
              : "Aucune machine virtuelle sur ce nœud."}
          </p>
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full border-collapse text-[12px]">
              <thead>
                <tr className="text-left text-[11px] text-text-muted">
                  <th className="py-1.5 pr-3 font-normal">ID</th>
                  <th className="py-1.5 pr-3 font-normal">Nom</th>
                  <th className="py-1.5 pr-3 font-normal">CPU</th>
                  <th className="py-1.5 pr-3 font-normal">RAM</th>
                  <th className="py-1.5 font-normal">État</th>
                </tr>
              </thead>
              <tbody>
                {guests.map((guest) => (
                  <tr key={guest.vmid} className="border-t-[0.5px] border-border">
                    <td className="py-1.5 pr-3 tabular-nums text-text-secondary">
                      {guest.vmid}
                    </td>
                    <td className="py-1.5 pr-3 text-text-primary">
                      {truncateGuestLabel(guest.vmid, guest.name)}
                    </td>
                    <td className="py-1.5 pr-3 tabular-nums text-text-secondary">
                      {guest.status === "template" ? "—" : formatRatio(guest.cpu.ratio)}
                    </td>
                    <td className="py-1.5 pr-3 tabular-nums text-text-secondary">
                      {guest.status === "template"
                        ? "—"
                        : formatBytes(guest.memory.used)}
                    </td>
                    <td className="py-1.5">
                      {guest.status === "template" ? (
                        <Tag>template</Tag>
                      ) : (
                        <Tag
                          variant={guest.status === "running" ? "success" : "neutral"}
                          icon={<StatusDot status={guest.status} title="" />}
                        >
                          {guest.status === "running" ? "En cours" : "Arrêtée"}
                        </Tag>
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </section>
    </div>
  );
}

function formatLoad(load: [number, number, number] | null): string {
  if (load === null) {
    return "—";
  }
  return load.map((value) => value.toFixed(2).replace(".", ",")).join(" · ");
}

function formatQuorum(node: NodeDetailData): string | null {
  if (node.quorum === null) {
    // A standalone node has no quorum; inventing one would be a lie.
    return "Nœud seul";
  }
  const { quorate, online, nodes } = node.quorum;
  return `${quorate ? "OK" : "Perdu"} · ${String(online)}/${String(nodes)} votes`;
}
