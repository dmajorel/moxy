import type { GuestNet } from "@/api/types";
import type { DataColumn } from "@/components/ui";
import { DataTable, Tag } from "@/components/ui";
import { useT } from "@/i18n/locale";
import { FALLBACK, formatVlan } from "@/lib/format";

/**
 * The networks a guest is wired to, one row per interface.
 *
 * The card used to say nothing at all about the wiring, and the native
 * interface says `vmbr12` — an identifier an operator has to translate from
 * memory, or go and look up elsewhere, at the exact moment they are checking
 * that a machine sits on the right network before a migration or a firewall
 * change. The name someone gave that network is what this table puts first.
 *
 * One rule carries it, and it is the opposite of the table next door: a
 * network with no alias is NOT unknown. Most bridges carry none, and falling
 * back to the em dash would replace a perfectly usable `vmbr0` with nothing.
 * The bridge is the answer when there is no better one; the em dash is kept
 * for a card attached to nothing at all.
 */
export interface GuestNetsTableProps {
  nets: GuestNet[];
  /** Shown when the guest declares no interface at all. */
  emptyHint?: string;
  className?: string;
}

/** An unrecorded figure: the em dash, muted, never a zero. */
function unknown() {
  return <span className="text-text-muted">{FALLBACK}</span>;
}

/**
 * The name of the network, with the identifier underneath.
 *
 * The alias leads because it is the thing being looked for; the bridge stays
 * visible because it is what every other tool — `qm config`, the native UI, a
 * firewall rule — will name. Without an alias the bridge takes the lead rather
 * than leaving the cell blank.
 */
function network(net: GuestNet) {
  if (net.bridge === null) {
    return unknown();
  }
  const vlan = formatVlan(net.tag);
  return (
    <div className="flex flex-wrap items-baseline gap-x-2 gap-y-0.5">
      <span className={net.alias === null ? "font-mono" : undefined}>
        {net.alias ?? net.bridge}
      </span>
      {net.alias === null ? null : (
        <span className="font-mono text-[11px] text-text-muted">{net.bridge}</span>
      )}
      {vlan === null ? null : <Tag className="font-sans">{vlan}</Tag>}
    </div>
  );
}

export function GuestNetsTable({ nets, emptyHint, className }: GuestNetsTableProps) {
  const t = useT();

  const columns: DataColumn[] = [
    { key: "slot", header: t("nets.column.slot"), mono: true, nowrap: true },
    { key: "network", header: t("nets.column.network") },
    {
      key: "mac",
      header: t("nets.column.mac"),
      mono: true,
      tone: "secondary",
      nowrap: true,
    },
  ];

  return (
    <DataTable
      caption={t("nets.caption")}
      columns={columns}
      emptyHint={emptyHint ?? t("nets.empty")}
      className={className}
      rows={nets.map((net) => ({
        key: net.key,
        cells: {
          // A container names the interface its own system will see; a VM
          // never does, and inventing "eth0" for one would be a guess.
          slot: (
            <>
              {net.key}
              {net.name === null ? null : (
                <span className="ml-2 text-text-muted">{net.name}</span>
              )}
            </>
          ),
          network: network(net),
          mac: net.mac ?? unknown(),
        },
      }))}
    />
  );
}
