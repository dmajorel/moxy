import { useCallback, useEffect, useRef, useState } from "react";
import type { KeyboardEvent } from "react";
import { IconBell } from "@tabler/icons-react";

import type { Alert, ClusterStatus } from "@/api/types";
import { StatusDot, Tag } from "@/components/ui";
import { formatAlert } from "@/lib/format";

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
  const [isOpen, setIsOpen] = useState(false);
  const [activeIndex, setActiveIndex] = useState(0);
  const rootRef = useRef<HTMLDivElement>(null);
  const buttonRef = useRef<HTMLButtonElement>(null);
  const itemRefs = useRef<(HTMLButtonElement | null)[]>([]);

  const count = alerts.length;
  const label =
    count > 0 ? `Notifications · ${count} alerte${count > 1 ? "s" : ""}` : "Notifications";

  const close = useCallback((restoreFocus: boolean) => {
    setIsOpen(false);
    if (restoreFocus) {
      buttonRef.current?.focus();
    }
  }, []);

  // Same rule as the cluster switcher: a menu that outlives a click elsewhere
  // is a trap, and focus only goes back to the bell when it was still inside.
  useEffect(() => {
    if (!isOpen) {
      return;
    }
    function onMouseDown(event: MouseEvent) {
      const root = rootRef.current;
      if (root === null || root.contains(event.target as Node)) {
        return;
      }
      setIsOpen(false);
      if (root.contains(document.activeElement)) {
        buttonRef.current?.focus();
      }
    }
    document.addEventListener("mousedown", onMouseDown);
    return () => {
      document.removeEventListener("mousedown", onMouseDown);
    };
  }, [isOpen]);

  // Arrow keys move focus for real, rather than only painting a highlight.
  useEffect(() => {
    if (isOpen && count > 0) {
      itemRefs.current[activeIndex]?.focus();
    }
  }, [isOpen, activeIndex, count]);

  function choose(clusterId: string) {
    onSelectCluster(clusterId);
    close(true);
  }

  function onKeyDown(event: KeyboardEvent<HTMLDivElement>) {
    if (!isOpen) {
      if (event.key === "ArrowDown" || event.key === "ArrowUp") {
        event.preventDefault();
        setActiveIndex(0);
        setIsOpen(true);
      }
      return;
    }
    switch (event.key) {
      case "Escape":
        event.preventDefault();
        close(true);
        break;
      case "ArrowDown":
        event.preventDefault();
        if (count > 0) setActiveIndex((index) => (index + 1) % count);
        break;
      case "ArrowUp":
        event.preventDefault();
        if (count > 0) setActiveIndex((index) => (index - 1 + count) % count);
        break;
      case "Home":
        event.preventDefault();
        setActiveIndex(0);
        break;
      case "End":
        event.preventDefault();
        setActiveIndex(Math.max(0, count - 1));
        break;
      case "Enter":
      case " ": {
        // preventDefault also cancels the browser's own activation of the
        // focused button, so the cluster is not opened twice.
        event.preventDefault();
        const entry = alerts[activeIndex];
        if (entry !== undefined) {
          choose(entry.clusterId);
        }
        break;
      }
      case "Tab":
        close(false);
        break;
      default:
        break;
    }
  }

  const classes = ["relative", className].filter(Boolean).join(" ");

  return (
    // The keydown sits on the wrapper because it routes for the button and the
    // menu items inside it, which are the interactive elements. The wrapper
    // itself is neither focusable nor clickable.
    // eslint-disable-next-line jsx-a11y/no-static-element-interactions -- event delegation
    <div ref={rootRef} className={classes} onKeyDown={onKeyDown}>
      <button
        ref={buttonRef}
        type="button"
        className={BUTTON_CLASSES}
        aria-label={label}
        aria-haspopup="menu"
        aria-expanded={isOpen}
        onClick={() => {
          if (isOpen) {
            close(false);
          } else {
            setActiveIndex(0);
            setIsOpen(true);
          }
        }}
      >
        <IconBell size={18} stroke={1.75} aria-hidden />
        {/* No pill at zero: a counter reading 0 is noise on a healthy estate. */}
        {count > 0 ? (
          <Tag variant="warning" className="px-1.5">
            {count}
          </Tag>
        ) : null}
      </button>

      {isOpen ? (
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
                ref={(element) => {
                  itemRefs.current[index] = element;
                }}
                type="button"
                role="menuitem"
                tabIndex={-1}
                className={ITEM_CLASSES}
                onClick={() => {
                  choose(entry.clusterId);
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
