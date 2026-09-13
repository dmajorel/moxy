import { IconBell } from "@tabler/icons-react";

import type { Alert, ClusterStatus } from "@/api/types";
import { StatusDot, Tag } from "@/components/ui";
import { formatAlert } from "@/lib/format";
import { useMenu } from "@/lib/useMenu";

/**
 * The bell, and what it opens.
 *
 * It used to be a <button> whose onClick was never supplied: it counted the
 * alerts of the whole estate and did nothing when clicked. A control that
 * shows a number and refuses to say more is worse than no control — the
 * operator concludes the tool is broken, which is the same rule that removed
 * "Ajouter un cluster".
 *
 * What it opens is the one list this application could not otherwise show:
 * each card carries the alerts of its own cluster, and the header counts them
 * all. This is where they are gathered, every cluster at once, each line
 * leading to the cluster it belongs to.
 */
export interface AlertEntry {
  clusterId: string;
  clusterName: string;
  status: ClusterStatus;
  alert: Alert;
}

export interface AlertsPanelProps {
  alerts: AlertEntry[];
  /** Opens the cluster an alert belongs to, and closes the panel. */
  onSelectCluster: (id: string) => void;
  className?: string;
}

const BUTTON_CLASSES =
  "flex flex-none items-center gap-1 rounded-card px-1 py-1 text-text-secondary " +
  "hover:bg-fill-ghost-selected focus:outline-none focus-visible:outline-1 " +
  "focus-visible:outline-accent";

const ITEM_CLASSES =
  "flex w-full items-start gap-2 rounded-card px-2 py-1.5 text-left text-[12px] " +
  "hover:bg-fill-ghost-selected focus:outline-none focus-visible:outline-1 " +
  "focus-visible:outline-accent";

export function AlertsPanel({ alerts, onSelectCluster, className }: AlertsPanelProps) {
  const count = alerts.length;
  const label =
    count > 0 ? `Notifications · ${count} alerte${count > 1 ? "s" : ""}` : "Notifications";

  const menu = useMenu({
    count,
    onActivate: (index) => {
      const entry = alerts[index];
      if (entry !== undefined) {
        onSelectCluster(entry.clusterId);
      }
    },
  });

  const classes = ["relative", className].filter(Boolean).join(" ");

  return (
    // The keydown sits on the wrapper because it routes for the button and the
    // menu items inside it, which are the interactive elements. The wrapper
    // itself is neither focusable nor clickable.
    // eslint-disable-next-line jsx-a11y/no-static-element-interactions -- event delegation
    <div ref={menu.rootRef} className={classes} onKeyDown={menu.onKeyDown}>
      <button
        {...menu.buttonProps}
        type="button"
        className={BUTTON_CLASSES}
        aria-label={label}
      >
        <IconBell size={18} stroke={1.75} aria-hidden />
        {/* No pill at zero: a counter reading 0 is noise on a healthy estate. */}
        {count > 0 ? (
          <Tag variant="warning" className="px-1.5">
            {count}
          </Tag>
        ) : null}
      </button>

      {menu.isOpen ? (
        <div
          role="menu"
          aria-label="Alertes"
          className="absolute top-full right-0 z-20 mt-1 max-h-[60vh] w-[320px] overflow-y-auto rounded-card border-[0.5px] border-border bg-surface-0 p-1"
        >
          {count === 0 ? (
            <p className="px-2 py-1.5 text-[12px] text-text-muted">Aucune alerte</p>
          ) : (
            alerts.map((entry, index) => (
              <button
                key={`${entry.clusterId}:${entry.alert.kind}:${String(index)}`}
                ref={menu.itemRef(index)}
                type="button"
                role="menuitem"
                tabIndex={-1}
                className={ITEM_CLASSES}
                onClick={() => {
                  menu.activate(index);
                }}
              >
                <StatusDot status={entry.status} decorative className="mt-[3px]" />
                <span className="min-w-0">
                  <span className="block truncate text-text-primary">
                    {formatAlert(entry.alert)}
                  </span>
                  <span className="block truncate text-[11px] text-text-muted">
                    {entry.clusterName}
                  </span>
                </span>
              </button>
            ))
          )}
        </div>
      ) : null}
    </div>
  );
}
