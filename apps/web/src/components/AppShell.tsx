import { useCallback, useEffect, useRef, useState } from "react";
import type { CSSProperties, KeyboardEvent, PointerEvent, ReactNode } from "react";

import { ErrorBoundary } from "@/components/ErrorBoundary";

/**
 * The application frame: top bar across the full width, then the two columns
 * of the appendix A.2 mock — the tree on the left, the contextual pane on the
 * right. The tree column starts at the nominal 190px of the mock and the user
 * may widen or narrow it from the separator sitting between the two columns.
 *
 * It is purely structural. Regions arrive as props and the shell knows nothing
 * of what they contain, so the tree, the top bar and the screens can be written
 * and tested on their own.
 */
export interface AppShellProps {
  /** Rendered full width above the two columns; it never scrolls away. */
  topBar: ReactNode;
  /** The cluster tree. Scrolls independently from the content. */
  sidebar: ReactNode;
  /** The contextual pane. Scrolls independently from the sidebar. */
  children: ReactNode;
  className?: string;
}

/** Target of the skip link; also the scroll container of the content pane. */
const CONTENT_ID = "content";

/** The nominal tree width of appendix A.2, and the width a reset returns to. */
export const DEFAULT_SIDEBAR_WIDTH = 190;

/**
 * Bounds of the drag. Below the minimum the truncated guest labels of the tree
 * ("103 · airflow-sep-exp") stop being readable; above the maximum the tree
 * starts eating the pane it is supposed to navigate.
 */
export const MIN_SIDEBAR_WIDTH = 150;
export const MAX_SIDEBAR_WIDTH = 420;

/** One arrow key press. Coarse enough to cross the range without impatience. */
const KEYBOARD_STEP = 16;

/**
 * A second, viewport-relative cap applied in CSS rather than in state: on a
 * narrow window a stored width of 420px would leave nothing for the content,
 * and no resize listener is needed to keep that from happening.
 */
const MAX_VIEWPORT_SHARE = "45vw";

/** Where the width survives a reload. Namespaced to keep the origin tidy. */
const STORAGE_KEY = "moxy.sidebar-width";

/** French, sentence case, like every other label of the UI. */
const HANDLE_LABEL = "Largeur du panneau de navigation";

/** Keeps a width inside the bounds; anything unusable falls back to the default. */
function clampWidth(value: number): number {
  if (!Number.isFinite(value)) return DEFAULT_SIDEBAR_WIDTH;
  return Math.min(MAX_SIDEBAR_WIDTH, Math.max(MIN_SIDEBAR_WIDTH, Math.round(value)));
}

/**
 * Reading localStorage throws outright in a private window or when site data is
 * blocked, so both accessors are guarded and the default width is the fallback
 * for every failure — a missing entry, an unparsable one, or no storage at all.
 */
function readStoredWidth(): number {
  try {
    const stored = window.localStorage.getItem(STORAGE_KEY);
    if (stored === null) return DEFAULT_SIDEBAR_WIDTH;
    return clampWidth(Number.parseInt(stored, 10));
  } catch {
    return DEFAULT_SIDEBAR_WIDTH;
  }
}

function writeStoredWidth(width: number): void {
  try {
    window.localStorage.setItem(STORAGE_KEY, String(width));
  } catch {
    // Not being able to remember the width is not worth breaking the layout.
  }
}

/**
 * The width an arrow key asks for, or null when the key is none of ours and the
 * event must be left to the browser.
 */
function widthForKey(key: string, current: number): number | null {
  switch (key) {
    case "ArrowLeft":
      return current - KEYBOARD_STEP;
    case "ArrowRight":
      return current + KEYBOARD_STEP;
    case "Home":
      return MIN_SIDEBAR_WIDTH;
    case "End":
      return MAX_SIDEBAR_WIDTH;
    default:
      return null;
  }
}

/**
 * Visible only while focused: `sr-only` keeps it out of the layout, and the
 * `focus:` variants bring it back as a real chip over the top bar.
 */
const SKIP_LINK_CLASSES =
  "sr-only focus:not-sr-only focus:absolute focus:left-2 focus:top-2 focus:z-50 " +
  "focus:rounded-card focus:border-[0.5px] focus:border-border focus:bg-surface-0 " +
  "focus:px-3 focus:py-2 focus:text-[12px] focus:text-text-primary " +
  "focus:outline-none focus:ring-1 focus:ring-accent";

/**
 * The viewport height lives here and `min-h-0` on the grid and its cells is
 * what lets the two columns — and only them — own the scrollbars.
 */
const ROOT_CLASSES = "relative flex h-screen flex-col bg-surface-0";

/**
 * The grab area straddles the hairline border of the sidebar: it is wider than
 * what it paints so it stays easy to catch with a mouse. Out of flow, so the
 * grid keeps its two columns.
 */
const HANDLE_CLASSES =
  "absolute inset-y-0 left-[var(--sidebar-width)] z-10 w-1.5 -translate-x-1/2 " +
  "cursor-col-resize touch-none bg-transparent hover:bg-accent " +
  "focus-visible:bg-accent focus-visible:outline-1 focus-visible:outline-accent";

export function AppShell({ topBar, sidebar, children, className }: AppShellProps) {
  const [width, setWidth] = useState(readStoredWidth);
  const [dragging, setDragging] = useState(false);
  const gridRef = useRef<HTMLDivElement>(null);
  /** The width the pointer listeners see, which are set up once per drag. */
  const widthRef = useRef(width);

  const applyWidth = useCallback((next: number) => {
    const clamped = clampWidth(next);
    widthRef.current = clamped;
    setWidth(clamped);
    return clamped;
  }, []);

  useEffect(() => {
    if (!dragging) return;

    // The sidebar starts at the left edge of the grid, so the width the pointer
    // is asking for is its distance to that edge.
    const origin = gridRef.current?.getBoundingClientRect().left ?? 0;

    const move = (event: globalThis.PointerEvent) => {
      applyWidth(event.clientX - origin);
    };
    const stop = () => {
      setDragging(false);
      writeStoredWidth(widthRef.current);
    };

    window.addEventListener("pointermove", move);
    window.addEventListener("pointerup", stop);
    window.addEventListener("pointercancel", stop);
    return () => {
      window.removeEventListener("pointermove", move);
      window.removeEventListener("pointerup", stop);
      window.removeEventListener("pointercancel", stop);
    };
  }, [dragging, applyWidth]);

  const handlePointerDown = (event: PointerEvent<HTMLDivElement>) => {
    // Secondary buttons open menus; only the primary one resizes.
    if (event.button !== 0) return;
    // Without this the drag selects the text of both columns as it crosses them.
    event.preventDefault();
    setDragging(true);
  };

  const handleKeyDown = (event: KeyboardEvent<HTMLDivElement>) => {
    const next = widthForKey(event.key, width);
    if (next === null) return;
    event.preventDefault();
    writeStoredWidth(applyWidth(next));
  };

  /** The usual affordance of a splitter: double-click returns to the nominal width. */
  const handleDoubleClick = () => {
    writeStoredWidth(applyWidth(DEFAULT_SIDEBAR_WIDTH));
  };

  const classes = [ROOT_CLASSES, dragging ? "select-none" : "", className]
    .filter(Boolean)
    .join(" ");

  // The one dimension the layout cannot know statically. A custom property, so
  // the grid and the separator read the same value from their own classes.
  const style: CSSProperties & Record<"--sidebar-width", string> = {
    "--sidebar-width": `min(${width}px, ${MAX_VIEWPORT_SHARE})`,
  };

  return (
    <div className={classes} style={style}>
      <a className={SKIP_LINK_CLASSES} href={`#${CONTENT_ID}`}>
        Aller au contenu
      </a>

      {/* Fixed: only the two columns below scroll. */}
      <div className="shrink-0">{topBar}</div>

      <div
        ref={gridRef}
        className="relative grid min-h-0 flex-1 grid-cols-[var(--sidebar-width)_minmax(0,1fr)]"
      >
        <aside
          aria-label="Clusters"
          className="min-h-0 overflow-y-auto border-r-[0.5px] border-border bg-surface-1 px-2 py-2.5"
        >
          {sidebar}
        </aside>

        <div
          role="separator"
          aria-orientation="vertical"
          aria-label={HANDLE_LABEL}
          aria-valuenow={width}
          aria-valuemin={MIN_SIDEBAR_WIDTH}
          aria-valuemax={MAX_SIDEBAR_WIDTH}
          tabIndex={0}
          className={HANDLE_CLASSES}
          onPointerDown={handlePointerDown}
          onKeyDown={handleKeyDown}
          onDoubleClick={handleDoubleClick}
        />

        {/* tabIndex -1 so the skip link really moves keyboard focus here. */}
        <main
          id={CONTENT_ID}
          tabIndex={-1}
          className="min-h-0 overflow-y-auto bg-surface-0 px-4 py-[14px] outline-none"
        >
          {/*
            The boundary sits HERE, inside the content pane, and not around
            the application: a screen that throws must not take the top bar
            and the tree with it. The operator keeps a way to navigate
            somewhere else, which is the whole difference between a broken
            screen and a blank page.
          */}
          <ErrorBoundary label="the content pane">{children}</ErrorBoundary>
        </main>
      </div>
    </div>
  );
}
