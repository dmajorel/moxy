import type { GuestDetail as GuestDetailData, Series, Task } from "@/api/types";
import { ObjectHeader } from "@/components/ObjectHeader";
import { TasksTable } from "@/components/TasksTable";
import { KeyValue, MetricCard, Sparkline } from "@/components/ui";
import { formatBytes, formatRatio, formatUptime, formatUsage } from "@/lib/format";

/**
 * Guest view — screen 1 of the mockups.
 *
 * Like the node view it has no tab bar: only the summary exists so far.
 */
export interface GuestDetailProps {
  guest: GuestDetailData;
  clusterName: string;
  series: Series | null;
  /** Cluster journal, already filtered to this guest by the caller. */
  tasks: Task[];
  threshold: number;
  className?: string;
}

const STATE_LABELS: Record<GuestDetailData["status"], string> = {
  running: "En cours",
  stopped: "Arrêtée",
  template: "Template",
};

export function GuestDetail({
  guest,
  clusterName,
  series,
  tasks,
  threshold,
  className,
}: GuestDetailProps) {
  const stateLabel =
    guest.status === "running"
      ? `${STATE_LABELS.running} · ${formatUptime(guest.uptime)}`
      : STATE_LABELS[guest.status];

  return (
    <div className={className}>
      <ObjectHeader
        breadcrumb={[clusterName, guest.node, `VM ${String(guest.vmid)}`]}
        name={guest.name}
        status={guest.status}
        stateLabel={stateLabel}
        chips={[guest.kind === "lxc" ? "Conteneur LXC" : "Machine virtuelle", ...guest.tags]}
      />

      <div className="mb-3.5 grid grid-cols-1 gap-2.5 sm:grid-cols-3">
        <MetricCard
          label="CPU"
          value={formatRatio(guest.cpu.ratio)}
          detail={`· ${String(guest.cpu.cores)} vCPU`}
          ratio={guest.cpu.ratio}
          threshold={threshold}
        />
        <MetricCard
          label="Mémoire"
          value={formatUsage(guest.memory)}
          ratio={guest.memory.ratio}
          threshold={threshold}
        />
        <MetricCard
          label="Disque de boot"
          // Proxmox only knows what a guest consumes when the agent reports it,
          // so a zero here means "not reported", not "empty".
          value={guest.disk.used === 0 ? formatBytes(guest.disk.total) : formatUsage(guest.disk)}
          detail={guest.disk.used === 0 ? "· alloué" : undefined}
          ratio={guest.disk.used === 0 ? undefined : guest.disk.ratio}
          threshold={threshold}
        />
      </div>

      <div className="mb-3.5 grid grid-cols-1 gap-2.5 lg:grid-cols-[minmax(0,1.5fr)_minmax(0,1fr)]">
        <section className="rounded-card border-[0.5px] border-border bg-surface-2 px-3 py-2.5">
          <div className="mb-1.5 flex items-center justify-between gap-3">
            <h2 className="text-[12px] font-medium text-text-primary">Charge CPU</h2>
            <span className="text-[11px] text-text-muted">
              {series === null
                ? "Dernière heure"
                : `Dernière heure · moy. ${formatRatio(series.cpuAverage)}`}
            </span>
          </div>
          <Sparkline points={series?.points ?? []} label={`Charge CPU de ${guest.name}`} />
        </section>

        <section className="rounded-card border-[0.5px] border-border bg-surface-2 px-3 py-1">
          <KeyValue
            rows={[
              { label: "Nœud", value: guest.node },
              { label: "HA", value: guest.haState },
              {
                label: "Mémoire hôte",
                value:
                  guest.hostMemory === null ? null : formatBytes(guest.hostMemory),
              },
              { label: "IPv4", value: guest.ipv4, mono: true },
            ]}
          />
        </section>
      </div>

      <section className="rounded-card border-[0.5px] border-border bg-surface-2 px-3 py-2.5">
        <div className="mb-1.5 flex flex-wrap items-baseline gap-3">
          <h2 className="text-[12px] font-medium text-text-primary">Tâches récentes</h2>
          <span className="text-[11px] text-text-muted">
            Journal du cluster · filtré sur cette machine
          </span>
        </div>
        <TasksTable
          entries={tasks}
          emptyHint="Aucune tâche récente pour cette machine."
        />
      </section>
    </div>
  );
}
