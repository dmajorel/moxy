import type { GuestDetail as GuestDetailData, Series, Task } from "@/api/types";
import { GuestDisksTable } from "@/components/GuestDisksTable";
import { ObjectHeader } from "@/components/ObjectHeader";
import { TasksTable } from "@/components/TasksTable";
import { KeyValue, MetricCard, Sparkline } from "@/components/ui";
import {
  formatAllocationQualifier,
  formatBytes,
  formatDetachedVolumes,
  formatDiskCount,
  formatRatio,
  formatUptime,
  formatUsage,
  splitTag,
} from "@/lib/format";
import { cpuRatios } from "@/lib/series";

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
  const detachedNote = formatDetachedVolumes(guest.allocated);
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
        // The name line is reserved for the state. A real fleet puts five to
        // ten tags on a guest, which would push the state out of sight and
        // wrap the header over three lines; they get a list of their own
        // below, where their keys line up.
        chips={[guest.kind === "lxc" ? "Conteneur LXC" : "Machine virtuelle"]}
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
        {guest.allocated === null ? (
          <MetricCard
            label="Disque de boot"
            // The configuration could not be read — no VM.Audit on this guest —
            // so the boot disk of the status endpoint is all there is. Proxmox
            // only knows what a guest consumes when the agent reports it, so a
            // zero here means "not reported", not "empty".
            value={
              guest.disk.used === 0 ? formatBytes(guest.disk.total) : formatUsage(guest.disk)
            }
            detail={guest.disk.used === 0 ? "· alloué" : undefined}
            ratio={guest.disk.used === 0 ? undefined : guest.disk.ratio}
            threshold={threshold}
          />
        ) : (
          <MetricCard
            label="Volumétrie"
            // Every volume the guest declares, not the boot disk alone. No bar:
            // what a guest consumes across its disks is unknown unless an agent
            // says so, and a fill level drawn from nothing would be a guess.
            value={formatBytes(guest.allocated.bytes)}
            detail={formatAllocationQualifier(guest.allocated)}
          />
        )}
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
          <Sparkline
            series={[{ values: cpuRatios(series?.points ?? []) }]}
            label={`Charge CPU de ${guest.name}`}
          />
        </section>

        <section className="rounded-card border-[0.5px] border-border bg-surface-2 px-3 py-1">
          <KeyValue
            rows={[
              { label: "Nœud", value: guest.node },
              { label: "HA", value: guest.haState },
              // What the guest itself uses of its boot disk, which only a
              // guest agent reports; a zero means "not reported". The line
              // only appears once the volumetry card has taken the metric
              // slot: without a readable configuration that card already IS
              // the boot disk, and saying it twice would suggest two figures.
              ...(guest.allocated === null
                ? []
                : [
                    {
                      label: "Disque de boot",
                      value: guest.disk.used === 0 ? null : formatUsage(guest.disk),
                    },
                  ]),
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

      {guest.disks === null ? null : (
        <section className="mb-3.5 rounded-card border-[0.5px] border-border bg-surface-2 px-3 py-2.5">
          <div className="mb-1.5 flex flex-wrap items-baseline gap-3">
            <h2 className="text-[12px] font-medium text-text-primary">Disques</h2>
            <span className="text-[11px] text-text-muted">
              {formatDiskCount(guest.disks.length)}
            </span>
          </div>
          <GuestDisksTable disks={guest.disks} />
          {detachedNote === null ? null : (
            <p className="mt-2 text-[11px] text-text-muted">
              {detachedNote} · hors total
            </p>
          )}
        </section>
      )}

      {guest.tags.length === 0 ? null : (
        <section className="mb-3.5 rounded-card border-[0.5px] border-border bg-surface-2 px-3 py-2.5">
          <h2 className="mb-0.5 text-[12px] font-medium text-text-primary">Étiquettes</h2>
          <KeyValue
            // PVE's own order, not an alphabetical one: sorting by key would
            // invent a hierarchy the fleet does not have. A flag tag — one
            // with nothing to cut — keeps the whole string as its key and
            // renders the em dash, which is what KeyValue does with a null.
            rows={guest.tags.map((tag) => {
              const { key, value } = splitTag(tag);
              return { label: key, value };
            })}
          />
        </section>
      )}

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
