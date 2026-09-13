import { useEffect, useRef } from "react";
import type { KeyboardEvent } from "react";
import { IconSearch } from "@tabler/icons-react";

import { AlertsPanel } from "@/components/AlertsPanel";
import type { AlertEntry } from "@/components/AlertsPanel";
import { Logo } from "@/components/ui";
import { ClusterSwitcher } from "@/components/ClusterSwitcher";
import type { ClusterSwitcherCluster } from "@/components/ClusterSwitcher";
import { ThemeToggle } from "@/components/ThemeToggle";
import type { ThemePreference } from "@/lib/theme";

/**
 * The application bar of section 2 of the handoff, rendered as HTML in annex
 * A.2: logo and version, cluster switcher, global search with its ⌘K shortcut,
 * notifications, theme control. No avatar -- see below for why.
 *
 * Everything here is controlled by the caller. The component holds no state, no
 * global store and no network call.
 */
export interface TopBarProps {
  /** Displayed next to the wordmark; omitted entirely when absent. */
  version?: string;
  clusters: ClusterSwitcherCluster[];
  /** null means "every cluster at once". */
  selectedClusterId: string | null;
  onSelectCluster: (id: string | null) => void;
  /** Content of the global search field. */
  value: string;
  onValueChange: (value: string) => void;
  /** Enter in the field: opens the first result. */
  onSubmitSearch?: () => void;
  /**
   * Every alert of every cluster, which the bell opens. The cards show the
   * alerts of one cluster each; this panel is the one place they are gathered,
   * every cluster at once.
   */
  alerts: AlertEntry[];
  /** Light, dark or "follow the system". Held by the application root. */
  themePreference: ThemePreference;
  onThemePreferenceChange: (preference: ThemePreference) => void;
  className?: string;
}

/*
 * No avatar. The handoff draws one, and it showed a "?" with the tooltip
 * "Authentification non configurée" -- a control standing for an identity that
 * does not exist. It comes back the day there is a name to put in it.
 */

// Tasks are not searchable: the field filters the tree, and the tree holds no
// task. Promising one in the placeholder is the same mistake as a button with
// no handler.
const SEARCH_PLACEHOLDER = "Rechercher une VM ou un nœud…";

/**
 * Which modifier the shortcut hint shows. The key handler accepts both Meta and
 * Control everywhere — an external keyboard on any machine can send either —
 * but the hint has to name the one the user actually has.
 *
 * navigator.platform is deprecated yet remains the only value every engine
 * still fills in; the user agent string is checked as well so that an iPad,
 * which reports "MacIntel" or "iPad" depending on the browser, is covered.
 */
function isApplePlatform(): boolean {
  if (typeof navigator === "undefined") {
    return false;
  }
  return /mac|iphone|ipad|ipod/i.test(
    `${navigator.platform} ${navigator.userAgent}`,
  );
}

const SEARCH_CLASSES =
  "mx-2 flex min-w-0 flex-1 items-center gap-2 rounded-card " +
  "border-[0.5px] border-border bg-surface-1 px-2.5 py-1.5";

const BASE_CLASSES =
  "flex items-center gap-3 border-b-[0.5px] border-border bg-surface-2 " +
  "px-[14px] py-[10px]";

export function TopBar({
  version,
  clusters,
  selectedClusterId,
  onSelectCluster,
  value,
  onValueChange,
  onSubmitSearch,
  alerts,
  themePreference,
  onThemePreferenceChange,
  className,
}: TopBarProps) {
  const inputRef = useRef<HTMLInputElement>(null);
  const shortcutHint = isApplePlatform() ? "⌘K" : "Ctrl+K";

  // ⌘K / Ctrl+K focuses the field from anywhere in the application.
  useEffect(() => {
    function onKeyDown(event: globalThis.KeyboardEvent) {
      if (event.key.toLowerCase() !== "k" || !(event.metaKey || event.ctrlKey)) {
        return;
      }
      event.preventDefault();
      const input = inputRef.current;
      if (input !== null) {
        input.focus();
        input.select();
      }
    }
    window.addEventListener("keydown", onKeyDown);
    return () => {
      window.removeEventListener("keydown", onKeyDown);
    };
  }, []);

  function onSearchKeyDown(event: KeyboardEvent<HTMLInputElement>) {
    if (event.key === "Escape") {
      event.preventDefault();
      // Clearing first, then blurring: Escape on a field that has already been
      // emptied is how one leaves it, and on a field that has not is how one
      // undoes the query without reaching for the mouse.
      if (value !== "") {
        onValueChange("");
        return;
      }
      event.currentTarget.blur();
    }
    if (event.key === "Enter") {
      event.preventDefault();
      onSubmitSearch?.();
    }
  }

  const classes = [BASE_CLASSES, className].filter(Boolean).join(" ");

  return (
    <header className={classes}>
      {/* The brand colour is reserved for this mark (section 2). */}
      <Logo />
      <span className="text-[14px] font-medium text-text-primary">moxy</span>
      {version === undefined ? null : (
        <span className="text-[12px] text-text-muted">{version}</span>
      )}

      <ClusterSwitcher
        clusters={clusters}
        selectedId={selectedClusterId}
        onSelect={onSelectCluster}
      />

      {/* The query filters the tree; Enter opens the first result. */}
      <div className={SEARCH_CLASSES}>
        <IconSearch
          className="flex-none text-text-muted"
          size={14}
          stroke={1.75}
          aria-hidden
        />
        <input
          ref={inputRef}
          type="search"
          value={value}
          aria-label="Recherche globale"
          placeholder={SEARCH_PLACEHOLDER}
          className="min-w-0 flex-1 bg-transparent text-[12px] text-text-primary outline-none placeholder:text-text-muted"
          onChange={(event) => {
            onValueChange(event.target.value);
          }}
          onKeyDown={onSearchKeyDown}
        />
        <span className="ml-auto flex-none font-mono text-[11px] text-text-muted">
          {shortcutHint}
        </span>
      </div>

      <AlertsPanel alerts={alerts} onSelectCluster={onSelectCluster} />

      <ThemeToggle
        preference={themePreference}
        onPreferenceChange={onThemePreferenceChange}
      />
    </header>
  );
}
