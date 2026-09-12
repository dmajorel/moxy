import type { ReactNode } from "react";

/**
 * Pill used for statuses, Proxmox tags and counters.
 *
 * Section 2 of the handoff: 11px, weight 500, 2px/8px padding, fully rounded.
 * Semantic variants carry meaning; `neutral` is the hairline-bordered chip used
 * for plain metadata (Proxmox tags, dates, totals).
 */
export type TagVariant = "success" | "warning" | "accent" | "neutral";

export interface TagProps {
  /** Defaults to "neutral": a tag only turns semantic on purpose. */
  variant?: TagVariant;
  /** Optional leading icon. The caller sizes it (11-13px reads best here). */
  icon?: ReactNode;
  className?: string;
  children?: ReactNode;
}

/**
 * A transparent border on the semantic variants keeps every tag the same height
 * as the bordered neutral one.
 */
const VARIANT_CLASSES: Record<TagVariant, string> = {
  success: "border-transparent bg-bg-success text-text-success",
  warning: "border-transparent bg-bg-warning text-text-warning",
  accent: "border-transparent bg-bg-accent text-text-accent",
  neutral: "border-border bg-surface-2 text-text-secondary",
};

const BASE_CLASSES =
  "inline-flex items-center gap-1 whitespace-nowrap rounded-full " +
  "border-[0.5px] px-2 py-[2px] text-[11px] leading-4 font-medium";

export function Tag({ variant = "neutral", icon, className, children }: TagProps) {
  const classes = [BASE_CLASSES, VARIANT_CLASSES[variant], className]
    .filter(Boolean)
    .join(" ");

  return (
    <span className={classes}>
      {icon === undefined || icon === null ? null : (
        <span className="flex shrink-0 items-center">{icon}</span>
      )}
      {children}
    </span>
  );
}
