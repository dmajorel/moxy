import type {
  GuestDetail as GuestDetailData,
  Series,
  Task,
  Thresholds,
  Timeframe,
} from "@/api/types";
import { GuestDisksTable } from "@/components/GuestDisksTable";
import { GuestNetsTable } from "@/components/GuestNetsTable";
import { ObjectHeader } from "@/components/ObjectHeader";
import { TasksTable } from "@/components/TasksTable";
import { ChartCard, KeyValue, MetricCard } from "@/components/ui";
import {
  formatAllocationQualifier,
  formatBytes,
  formatDetachedVolumes,
  formatDiskCount,
  formatNetCount,
  formatGuestKind,
  formatGuestRef,
  formatGuestStatus,
  formatHaState,
  formatRatio,
  formatUptime,
  formatUsageLine,
  formatUsageParts,
  formatVcpus,
  splitTag,
} from "@/lib/format";

/**
 * Guest view — screen 1 of the mockups.
 *
 * Like the node view it has no tab bar: only the summary exists so far.
 */
export interface GuestDetailProps {
  guest: GuestDetailData;
  clusterName: string;
  series: Series | null;
  /** The window asked for, which the picker shows as the current choice. */
  timeframe: Timeframe;
  /**
   * Picks another window. Omitted, the chart keeps the one it is given and no
   * picker is drawn — a radio group nobody listens to would be a dead control.
   */
  onTimeframeChange?: (timeframe: Timeframe) => void;
  /** The jobs filed against this guest, as its hosting node reports them. */
  tasks: Task[];
  /** One per resource: the boot disk is not coloured by the memory limit. */
  thresholds: Thresholds;
  className?: string;
}

export function GuestDetail({
  guest,
  clusterName,
  series,
  timeframe,
  onTimeframeChange,
  tasks,
  thresholds,
  className,
}: GuestDetailProps) {
  const detachedNote = formatDetachedVolumes(guest.allocated);
  // Volumes and networks share one row when both are readable. One 403 on the
  // configuration takes both away at once, so the pair is usually all or
  // nothing; when only one survives it takes the full width rather than
  // sitting in half a row with a hole beside it.
  const hasDisks = guest.disks !== null;
  const hasNets = guest.nets !== null;
  const inventoryColumns =
    hasDisks && hasNets ? "lg:grid-cols-[minmax(0,1.5fr)_minmax(0,1fr)]" : "";
  // The uptime is appended only when there is one. A stopped guest and a
  // template have none, and the payload says so with a null rather than with
  // a zero that would read as "started this second".
  const stateLabel =
    guest.uptime === null
      ? formatGuestStatus(guest.status)
      : `${formatGuestStatus(guest.status)} · ${formatUptime(guest.uptime)}`;

  const memory = formatUsageParts(guest.memory);
  const disk = formatUsageParts(guest.disk);

  return (
    <div className={className}>
      <ObjectHeader
        // "CT 105" for a container, as PVE and pct write it: a breadcrumb
        // calling an LXC "VM 105" contradicts every other tool the operator
        // uses.
        breadcrumb={[clusterName, guest.node, formatGuestRef(guest.kind, guest.vmid)]}
        name={guest.name}
        status={guest.status}
        stateLabel={stateLabel}
        // The name line is reserved for the state. A real fleet puts five to
        // ten tags on a guest, which would push the state out of sight and
        // wrap the header over three lines; they get a list of their own
        // below, where their keys line up.
        chips={[formatGuestKind(guest.kind)]}
      />

      <div className="mb-3.5 grid grid-cols-1 gap-2.5 sm:grid-cols-3">
        <MetricCard
          label="CPU"
          value={formatRatio(guest.cpu.ratio)}
          detail={`· ${formatVcpus(guest.cpu.cores)}`}
          ratio={guest.cpu.ratio}
          threshold={thresholds.cpu}
        />
        <MetricCard
          label="Mémoire"
          value={memory.value}
          detail={memory.detail}
          ratio={guest.memory.ratio}
          threshold={thresholds.memory}
        />
        {guest.allocated === null ? (
          <MetricCard
            label="Disque de boot"
            // The configuration could not be read — no VM.Audit on this guest —
            // so the boot disk of the status endpoint is all there is. Proxmox
            // only knows what a guest consumes when the agent reports it, and
            // says so with a null: the size is then all there is to show, and
            // there is no fill level to draw.
            value={
              guest.disk.used === null ? formatBytes(guest.disk.total) : disk.value
            }
            detail={guest.disk.used === null ? "· alloué" : disk.detail}
            ratio={guest.disk.ratio ?? undefined}
            threshold={thresholds.storage}
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
        <ChartCard
          title="Charge CPU"
          label={`Charge CPU de ${guest.name}`}
          series={series}
          timeframe={timeframe}
          onTimeframeChange={onTimeframeChange}
        />

        <section className="rounded-card border-[0.5px] border-border bg-surface-2 px-3 py-1">
          <KeyValue
            rows={[
              { label: "Nœud", value: guest.node },
              { label: "HA", value: formatHaState(guest.haState) },
              // What the guest itself uses of its boot disk, which only a
              // guest agent reports; null means "not reported". The line
              // only appears once the volumetry card has taken the metric
              // slot: without a readable configuration that card already IS
              // the boot disk, and saying it twice would suggest two figures.
              ...(guest.allocated === null
                ? []
                : [
                    {
                      label: "Disque de boot",
                      value:
                        guest.disk.used === null ? null : formatUsageLine(guest.disk),
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

      {!hasDisks && !hasNets ? null : (
        <div className={`mb-3.5 grid grid-cols-1 items-start gap-2.5 ${inventoryColumns}`}>
          {guest.disks === null ? null : (
            <section className="rounded-card border-[0.5px] border-border bg-surface-2 px-3 py-2.5">
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
          {guest.nets === null ? null : (
            <section className="rounded-card border-[0.5px] border-border bg-surface-2 px-3 py-2.5">
              <div className="mb-1.5 flex flex-wrap items-baseline gap-3">
                <h2 className="text-[12px] font-medium text-text-primary">Réseaux</h2>
                <span className="text-[11px] text-text-muted">
                  {formatNetCount(guest.nets.length)}
                </span>
              </div>
              <GuestNetsTable nets={guest.nets} />
            </section>
          )}
        </div>
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
          {/*
            Saying which node the log comes from is not decoration: the tasks a
            guest ran before it migrated stay on the node it left, so a history
            that starts abruptly has a reason the reader can see.
          */}
          <span className="text-[11px] text-text-muted">
            Tâches de cette machine sur {guest.node}
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
