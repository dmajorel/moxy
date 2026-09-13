import type { GuestDisk } from "@/api/types";
import { Tag } from "@/components/ui";
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

export function GuestDisksTable({ disks, emptyHint, className }: GuestDisksTableProps) {
  if (disks.length === 0) {
    return (
      <p className={["text-[12px] text-text-muted", className].filter(Boolean).join(" ")}>
        {emptyHint ?? "Ce système ne déclare aucun disque."}
      </p>
    );
  }

  return (
    <div className={["overflow-x-auto", className].filter(Boolean).join(" ")}>
      <table className="w-full border-collapse text-[12px]">
        <thead>
          <tr className="text-left text-[11px] text-text-muted">
            <th className="py-1.5 pr-3 font-normal">Emplacement</th>
            <th className="py-1.5 pr-3 font-normal">Stockage</th>
            <th className="py-1.5 pr-3 font-normal">Volume</th>
            <th className="py-1.5 text-right font-normal">Taille</th>
          </tr>
        </thead>
        <tbody>
          {disks.map((disk) => (
            <tr key={disk.key} className="border-t-[0.5px] border-border">
              <td className="py-1.5 pr-3 font-mono whitespace-nowrap text-text-primary">
                {disk.key}
                {disk.attached ? null : (
                  <Tag className="ml-2 font-sans" variant="warning">
                    Détaché
                  </Tag>
                )}
              </td>
              <td className="py-1.5 pr-3 whitespace-nowrap text-text-secondary">
                {disk.storage === null ? (
                  <span className="text-text-muted">{FALLBACK}</span>
                ) : (
                  disk.storage
                )}
              </td>
              <td className="py-1.5 pr-3 font-mono break-all text-text-secondary">
                {disk.volume}
              </td>
              <td className="py-1.5 text-right tabular-nums whitespace-nowrap text-text-primary">
                {disk.size === null ? (
                  <span className="text-text-muted">{FALLBACK}</span>
                ) : (
                  formatBytes(disk.size)
                )}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
