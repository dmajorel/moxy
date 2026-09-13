import type { GuestDisk } from "@/api/types";
import type { DataTableColumn } from "@/components/ui";
import { DataTable, Tag } from "@/components/ui";
import { FALLBACK, formatBytes } from "@/lib/format";

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

const COLUMNS: DataTableColumn<GuestDisk>[] = [
  {
    header: "Emplacement",
    cellClassName: "font-mono whitespace-nowrap text-text-primary",
    render: (disk) => (
      <>
        {disk.key}
        {disk.attached ? null : (
          <Tag className="ml-2 font-sans" variant="warning">
            Détaché
          </Tag>
        )}
      </>
    ),
  },
  {
    header: "Stockage",
    cellClassName: "whitespace-nowrap text-text-secondary",
    render: (disk) =>
      disk.storage === null ? (
        <span className="text-text-muted">{FALLBACK}</span>
      ) : (
        disk.storage
      ),
  },
  {
    header: "Volume",
    cellClassName: "font-mono break-all text-text-secondary",
    render: (disk) => disk.volume,
  },
  {
    header: "Taille",
    headClassName: "text-right",
    cellClassName: "text-right tabular-nums whitespace-nowrap text-text-primary",
    render: (disk) =>
      disk.size === null ? (
        <span className="text-text-muted">{FALLBACK}</span>
      ) : (
        formatBytes(disk.size)
      ),
  },
];

export function GuestDisksTable({ disks, emptyHint, className }: GuestDisksTableProps) {
  return (
    <DataTable
      caption="Volumes déclarés par cet invité"
      columns={COLUMNS}
      rows={disks}
      rowKey={(disk) => disk.key}
      emptyHint={emptyHint ?? "Ce système ne déclare aucun disque."}
      className={className}
    />
  );
}
