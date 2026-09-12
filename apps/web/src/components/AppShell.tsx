import type { ReactNode } from "react";

/**
 * The application frame: top bar across the full width, then the two columns
 * of the appendix A.2 mock — a fixed 190px tree on the left, the contextual
 * pane on the right.
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

export function AppShell({ topBar, sidebar, children, className }: AppShellProps) {
  const classes = [ROOT_CLASSES, className].filter(Boolean).join(" ");

  return (
    <div className={classes}>
      <a className={SKIP_LINK_CLASSES} href={`#${CONTENT_ID}`}>
        Aller au contenu
      </a>

      {/* Fixed: only the two columns below scroll. */}
      <div className="shrink-0">{topBar}</div>

      <div className="grid min-h-0 flex-1 grid-cols-[190px_minmax(0,1fr)]">
        <aside
          aria-label="Clusters"
          className="min-h-0 overflow-y-auto border-r-[0.5px] border-border bg-surface-1 px-2 py-2.5"
        >
          {sidebar}
        </aside>

        {/* tabIndex -1 so the skip link really moves keyboard focus here. */}
        <main
          id={CONTENT_ID}
          tabIndex={-1}
          className="min-h-0 overflow-y-auto bg-surface-0 px-4 py-[14px] outline-none"
        >
          {children}
        </main>
      </div>
    </div>
  );
}
