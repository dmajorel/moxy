import type { ReactNode } from "react";

import { StatusDot } from "@/components/ui";
import type { StatusDotStatus } from "@/components/ui";
import { Tag } from "@/components/ui";

/**
 * Heading of a node or guest view.
 *
 * Section 2 puts the state on the same line as the name, never buried in a
 * key/value list below: an operator opening a VM should know whether it is
 * running before reading anything else. Actions sit at the far right.
 */
export interface ObjectHeaderProps {
  /** Path above the name, e.g. ["Qualification", "Nœud"]. */
  breadcrumb: string[];
  name: string;
  status: StatusDotStatus;
  /** Already-formatted state label, e.g. "En ligne · 41 j". */
  stateLabel: string;
  /** Neutral chips: PVE version, Proxmox tags, guest counts. */
  chips?: string[];
  /** Rendered at the far right; omitted entirely when absent. */
  actions?: ReactNode;
  className?: string;
}

export function ObjectHeader({
  breadcrumb,
  name,
  status,
  stateLabel,
  chips = [],
  actions,
  className,
}: ObjectHeaderProps) {
  const stateVariant =
    status === "maintenance" || status === "degraded"
      ? "warning"
      : status === "online" || status === "running" || status === "healthy"
        ? "success"
        : "neutral";

  return (
    <header className={["mb-3", className].filter(Boolean).join(" ")}>
      {breadcrumb.length > 0 && (
        <p className="mb-1 text-[11px] text-text-muted">{breadcrumb.join(" › ")}</p>
      )}
      <div className="flex flex-wrap items-center gap-2">
        <h1 className="text-[18px] font-medium text-text-primary">{name}</h1>
        <Tag variant={stateVariant} icon={<StatusDot status={status} decorative />}>
          {stateLabel}
        </Tag>
        {chips.map((chip) => (
          <Tag key={chip}>{chip}</Tag>
        ))}
        {actions !== undefined && (
          <div className="ml-auto flex items-center gap-1.5">{actions}</div>
        )}
      </div>
    </header>
  );
}
