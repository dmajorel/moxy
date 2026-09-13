import type { ClusterStatus, GuestStatus, NodeStatus } from "@/api/types";
import { formatGuestStatus } from "@/lib/format";

/**
 * The 7px coloured dot that replaces the four state icons of the native UI
 * (section 2 of the handoff). It accepts every status the API models, so a
 * cluster, a node and a guest can all be rendered by the same primitive.
 */
export type StatusDotStatus = ClusterStatus | NodeStatus | GuestStatus;

export interface StatusDotProps {
  status: StatusDotStatus;
  /**
   * Alternative text. A bare coloured dot is invisible to a screen reader, so
   * one is always exposed: this prop overrides the default French label.
   */
  title?: string;
  /**
   * Marks the dot as pure decoration, next to a label that already says the
   * state in words.
   *
   * It is a flag and not an empty `title`, which is what the two callers were
   * passing: "" is not nullish, so it became `aria-label=""` on a role="img"
   * — an image with no name in the accessibility tree, which is a worse
   * failure than the duplicate reading it was avoiding.
   */
  decorative?: boolean;
  className?: string;
}

/** Colour is carried by a token, never by a literal. */
const TONE_CLASSES: Record<StatusDotStatus, string> = {
  healthy: "bg-success",
  online: "bg-success",
  running: "bg-success",
  degraded: "bg-warning",
  maintenance: "bg-warning",
  unreachable: "bg-text-muted",
  offline: "bg-text-muted",
  stopped: "bg-text-muted",
  template: "bg-text-muted",
  unknown: "bg-text-muted",
};

/** Displayed labels are French, sentence case. */
const DEFAULT_TITLES: Record<StatusDotStatus, string> = {
  healthy: "Sain",
  online: "En ligne",
  running: "En cours",
  degraded: "Dégradé",
  maintenance: "En maintenance",
  unreachable: "Injoignable",
  offline: "Hors ligne",
  stopped: "Arrêté",
  // The one word for that state, read from where it is decided rather than
  // spelled a third time: "template", "Template" and "Modèle" used to name it
  // in three different files.
  template: formatGuestStatus("template"),
  unknown: "État inconnu",
};

const BASE_CLASSES = "inline-block size-[7px] flex-none rounded-full";

export function StatusDot({ status, title, decorative, className }: StatusDotProps) {
  const label = title ?? DEFAULT_TITLES[status];
  const classes = [BASE_CLASSES, TONE_CLASSES[status], className]
    .filter(Boolean)
    .join(" ");

  if (decorative === true) {
    // No role, no name: the neighbouring text is the label, and an image
    // announced with nothing in it is noise in the middle of a sentence.
    return <span aria-hidden className={classes} />;
  }
  return <span className={classes} role="img" aria-label={label} title={label} />;
}
