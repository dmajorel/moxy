import { IconChevronDown } from "@tabler/icons-react";

import type { ClusterStatus } from "@/api/types";
import { StatusDot } from "@/components/ui";
import { plural } from "@/lib/format";
import { useMenu } from "@/lib/useMenu";

/**
 * The "3 clusters ▾" control of the top bar (section 2 of the handoff): it
 * switches the whole UI to one cluster, or back to the aggregated view.
 *
 * It owns nothing but its own open/closed state: the selection is controlled by
 * the caller, and no data is fetched here.
 */
export interface ClusterSwitcherCluster {
  id: string;
  name: string;
  status: ClusterStatus;
}

export interface ClusterSwitcherProps {
  clusters: ClusterSwitcherCluster[];
  /** null means "every cluster at once". */
  selectedId: string | null;
  onSelect: (id: string | null) => void;
  className?: string;
}

/** Displayed labels are French, sentence case. */
const ALL_LABEL = "Tous les clusters";

/**
 * Green only when nothing is wrong anywhere: a single degraded or unreachable
 * cluster has to be visible from the aggregated view, which is the whole point
 * of the dot.
 */
function aggregateStatus(clusters: ClusterSwitcherCluster[]): ClusterStatus {
  return clusters.every((cluster) => cluster.status === "healthy")
    ? "healthy"
    : "degraded";
}

/** "1 cluster", "3 clusters" — French pluralises from 2. */
function countLabel(count: number): string {
  return plural(count, "cluster", "clusters");
}

const BUTTON_CLASSES =
  "flex items-center gap-1.5 rounded-card border-[0.5px] border-border " +
  "bg-surface-2 px-2.5 py-1 text-[12px] text-text-primary " +
  "hover:bg-fill-ghost-selected focus-visible:outline-1 " +
  "focus-visible:outline-accent";

const ITEM_CLASSES =
  "flex w-full items-center gap-2 rounded-card px-2 py-1.5 text-left " +
  "text-[12px] text-text-secondary hover:bg-fill-ghost-selected " +
  "focus:bg-fill-ghost-selected focus:outline-none";

export function ClusterSwitcher({
  clusters,
  selectedId,
  onSelect,
  className,
}: ClusterSwitcherProps) {
  const aggregate = aggregateStatus(clusters);
  const selected =
    selectedId === null
      ? undefined
      : clusters.find((cluster) => cluster.id === selectedId);

  /** "Tous les clusters" is always the first entry, so index 0 means null. */
  const options: Array<{
    id: string | null;
    name: string;
    status: ClusterStatus;
  }> = [{ id: null, name: ALL_LABEL, status: aggregate }, ...clusters];

  const menu = useMenu({
    count: options.length,
    selectedIndex: options.findIndex((option) => option.id === selectedId),
    onActivate: (index) => {
      const option = options[index];
      if (option !== undefined) {
        onSelect(option.id);
      }
    },
  });

  const classes = ["relative", className].filter(Boolean).join(" ");

  return (
    // As in AlertsPanel: the wrapper routes the keys of the button and the
    // menu items it holds, and is itself neither focusable nor clickable.
    // eslint-disable-next-line jsx-a11y/no-static-element-interactions -- event delegation
    <div ref={menu.rootRef} className={classes} onKeyDown={menu.onKeyDown}>
      <button
        ref={menu.buttonRef}
        type="button"
        className={BUTTON_CLASSES}
        aria-haspopup="menu"
        aria-expanded={menu.isOpen}
        onClick={menu.toggle}
      >
        <StatusDot status={selected?.status ?? aggregate} />
        {selected?.name ?? countLabel(clusters.length)}
        <IconChevronDown
          className="text-text-muted"
          size={12}
          stroke={1.75}
          aria-hidden
        />
      </button>

      {menu.isOpen ? (
        <div
          role="menu"
          aria-label="Clusters"
          className="absolute top-full left-0 z-20 mt-1 min-w-[200px] rounded-card border-[0.5px] border-border bg-surface-0 p-1"
        >
          {options.map((option, index) => {
            const isSelected = option.id === selectedId;
            return (
              <button
                key={option.id ?? "all"}
                ref={menu.itemRef(index)}
                type="button"
                role="menuitemradio"
                aria-checked={isSelected}
                tabIndex={-1}
                className={[
                  ITEM_CLASSES,
                  isSelected ? "bg-bg-accent text-text-accent" : "",
                ]
                  .filter(Boolean)
                  .join(" ")}
                onClick={() => {
                  menu.activate(index);
                }}
              >
                <StatusDot status={option.status} />
                {option.name}
              </button>
            );
          })}
        </div>
      ) : null}
    </div>
  );
}
