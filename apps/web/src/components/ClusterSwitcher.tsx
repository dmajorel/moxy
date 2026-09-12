import { useCallback, useEffect, useRef, useState } from "react";
import type { KeyboardEvent } from "react";
import { IconChevronDown } from "@tabler/icons-react";

import type { ClusterStatus } from "@/api/types";
import { StatusDot } from "@/components/ui";

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
  return `${count} cluster${count > 1 ? "s" : ""}`;
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
  const [isOpen, setIsOpen] = useState(false);
  const [activeIndex, setActiveIndex] = useState(0);
  const rootRef = useRef<HTMLDivElement>(null);
  const buttonRef = useRef<HTMLButtonElement>(null);
  const itemRefs = useRef<Array<HTMLButtonElement | null>>([]);

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

  const close = useCallback((restoreFocus: boolean) => {
    setIsOpen(false);
    if (restoreFocus) {
      buttonRef.current?.focus();
    }
  }, []);

  function open() {
    const index = options.findIndex((option) => option.id === selectedId);
    setActiveIndex(index === -1 ? 0 : index);
    setIsOpen(true);
  }

  function choose(id: string | null) {
    onSelect(id);
    close(true);
  }

  // A menu that outlives a click elsewhere is a trap; focus only goes back to
  // the button when it was still inside the menu, so an outside click is free
  // to land wherever the user aimed.
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
    if (isOpen) {
      itemRefs.current[activeIndex]?.focus();
    }
  }, [isOpen, activeIndex]);

  function onKeyDown(event: KeyboardEvent<HTMLDivElement>) {
    if (!isOpen) {
      if (event.key === "ArrowDown" || event.key === "ArrowUp") {
        event.preventDefault();
        open();
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
        setActiveIndex((index) => (index + 1) % options.length);
        break;
      case "ArrowUp":
        event.preventDefault();
        setActiveIndex((index) => (index - 1 + options.length) % options.length);
        break;
      case "Home":
        event.preventDefault();
        setActiveIndex(0);
        break;
      case "End":
        event.preventDefault();
        setActiveIndex(options.length - 1);
        break;
      case "Enter":
      case " ": {
        // preventDefault also cancels the browser's own activation of the
        // focused button, so the selection is not applied twice.
        event.preventDefault();
        const option = options[activeIndex];
        if (option !== undefined) {
          choose(option.id);
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
    <div ref={rootRef} className={classes} onKeyDown={onKeyDown}>
      <button
        ref={buttonRef}
        type="button"
        className={BUTTON_CLASSES}
        aria-haspopup="menu"
        aria-expanded={isOpen}
        onClick={() => {
          if (isOpen) {
            close(false);
          } else {
            open();
          }
        }}
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

      {isOpen ? (
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
                ref={(element) => {
                  itemRefs.current[index] = element;
                }}
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
                  choose(option.id);
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
