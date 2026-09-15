import { IconServer, IconTool } from "@tabler/icons-react";

import type { NodeStatus } from "@/api/types";
import { useFormat } from "@/i18n/locale";

export interface NodeGlyphProps {
  status: NodeStatus;
  className?: string;
}

/**
 * The size of a node glyph, in px: the same in the sidebar tree and on a
 * cluster card, both of which set their rows in 12px. Two values would be two
 * chances to drift apart, and there is nothing to gain from either.
 */
const SIZE = 13;

/**
 * The colour of the glyph.
 *
 * Ink tokens, not the fill tokens a 7px dot would be painted with: a 1.75px
 * stroke is not a flat area, so it is held to the 4.5:1 of text rather than to
 * the 3:1 of a graphic. `--warning` is the documented exception of tokens.css —
 * 2.04:1 on `--surface-1` in the light theme — which is why a drained node is
 * amber through `--text-warning-strong`. All three clear 4.5:1 on every surface
 * of both themes, on the hover fill and on the selection fill.
 *
 * A drained node is amber and not grey because it is still online and still
 * voting: it merely refuses to take new guests.
 */
const TONE: Record<NodeStatus, string> = {
  online: "text-text-success",
  maintenance: "text-text-warning-strong",
  offline: "text-text-muted",
  unknown: "text-text-muted",
};

/**
 * One node, as a glyph: a server, or the wrench that REPLACES it while the node
 * is drained.
 *
 * The wrench replaces rather than joins, in both views, for two reasons that
 * were found separately and end up being the same one. On a cluster card the
 * dot could not say it — `StatusDot` paints a degraded node and a drained one
 * with the same amber, so the dot named a colour and a badge at the end of the
 * row did the naming. In the tree the wrench was tried as a 9px badge in the
 * corner of the server, and it did not read at all: a badge has no room to
 * exist at this size, and the hole punched to detach it cost a third of the
 * silhouette for nothing.
 *
 * So the shape of a node varies with its state, which section 2 argues against
 * when it drops the four state icons of the native UI. It varies ONCE, for the
 * one state that describes an operation somebody started rather than a degree
 * of health; the other three keep one shape and differ only in colour. The
 * amber and the label say it too, so nothing rests on the shape alone.
 *
 * ONE named image, never a glyph beside a dot, and never two marks for one
 * fact: a screen reader used to hear "Maintenance" from the dot and
 * "Maintenance planifiée" from the wrench, one after the other, for the same
 * node. The word is `formatNodeStatus`, the one the rest of the interface uses
 * for that state.
 */
export function NodeGlyph({ status, className }: NodeGlyphProps) {
  const { formatNodeStatus } = useFormat();
  const Icon = status === "maintenance" ? IconTool : IconServer;
  const label = formatNodeStatus(status);
  return (
    <Icon
      size={SIZE}
      stroke={1.75}
      className={["flex-none", TONE[status], className].filter(Boolean).join(" ")}
      role="img"
      aria-label={label}
      title={label}
    />
  );
}
