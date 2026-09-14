import type { GuestDisk } from "@/api/types";
import type { DataColumn } from "@/components/ui";
import { DataTable, Tag } from "@/components/ui";
import { FALLBACK, formatBytes, formatVolumeName } from "@/lib/format";

/**
 * The volumes a guest allocates, one row each.
 *
 * The native interface — and this view before it — showed a single figure, the
 * boot disk, which on a guest with a data disk is not even the largest of its
 * volumes. A VM with a 32 GiB system disk and a 2 TiB data disk read as
 * "32 GiB", and there was nowhere to see otherwise without leaving moxy.
 *
 * Two rules carry the table. A size nobody knows renders the em dash and never
 * a zero: a device passed straight through to the guest declares no size, and
 * neither does a detached volume, for which PVE records none. And a detached
 * volume is listed — it still occupies its storage — but marked, because it is
 * not part of what the running guest uses.
 */
export interface GuestDisksTableProps {
  disks: GuestDisk[];
  /** Shown when the guest declares no volume at all. */
  emptyHint?: string;
  className?: string;
}

const COLUMNS: DataColumn[] = [
  { key: "slot", header: "Emplacement", mono: true, nowrap: true },
  { key: "storage", header: "Stockage", nowrap: true, tone: "secondary" },
  { key: "volume", header: "Volume", mono: true, tone: "secondary", className: "break-all" },
  { key: "size", header: "Taille", align: "right", numeric: true, nowrap: true },
];

/** An unrecorded figure: the em dash, muted, never a zero. */
function unknown() {
  return <span className="text-text-muted">{FALLBACK}</span>;
}

export function GuestDisksTable({ disks, emptyHint, className }: GuestDisksTableProps) {
  return (
    <DataTable
      caption="Volumes déclarés par cet invité"
      columns={COLUMNS}
      emptyHint={emptyHint ?? "Ce système ne déclare aucun disque."}
      className={className}
      rows={disks.map((disk) => ({
        key: disk.key,
        cells: {
          slot: (
            <>
              {disk.key}
              {disk.attached ? null : (
                <Tag className="ml-2 font-sans" variant="warning">
                  Détaché
                </Tag>
              )}
            </>
          ),
          storage: disk.storage ?? unknown(),
          // The storage already has a column of its own; repeating it as a
          // prefix here spells it twice and wraps the part that identifies
          // the volume.
          volume: formatVolumeName(disk.volume, disk.storage),
          size: disk.size === null ? unknown() : formatBytes(disk.size),
        },
      }))}
    />
  );
}
