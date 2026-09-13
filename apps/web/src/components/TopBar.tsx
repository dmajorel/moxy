import { useEffect, useRef } from "react";
import type { KeyboardEvent } from "react";
import { IconBell, IconSearch } from "@tabler/icons-react";

import { Logo, Tag } from "@/components/ui";
import { ClusterSwitcher } from "@/components/ClusterSwitcher";
import type { ClusterSwitcherCluster } from "@/components/ClusterSwitcher";
import { ThemeToggle } from "@/components/ThemeToggle";
import type { ThemePreference } from "@/lib/theme";

/**
 * The application bar of section 2 of the handoff, rendered as HTML in annex
 * A.2: logo and version, cluster switcher, global search with its ⌘K shortcut,
 * notifications, theme control, user avatar.
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
  /** A counter pill is drawn on the bell above zero. */
  alertCount?: number;
  onAlertsClick?: () => void;
  /** Light, dark or "follow the system". Held by the application root. */
  themePreference: ThemePreference;
  onThemePreferenceChange: (preference: ThemePreference) => void;
  /** Two or three letters, e.g. "ro". */
  userInitials: string;
  /** Full name, exposed as the avatar's accessible name when given. */
  userName?: string;
  className?: string;
}

const SEARCH_PLACEHOLDER = "Rechercher une VM, un nœud, une tâche…";

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
  alertCount = 0,
  onAlertsClick,
  themePreference,
  onThemePreferenceChange,
  userInitials,
  userName,
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
      event.currentTarget.blur();
    }
  }

  const alertsLabel =
    alertCount > 0
      ? `Notifications · ${alertCount} alerte${alertCount > 1 ? "s" : ""}`
      : "Notifications";

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

      {/*
       * Search is a controlled field only at this stage: it carries the query
       * up, and the actual filtering of VMs, nodes and tasks lands in a later
       * step.
       */}
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

      <button
        type="button"
        className="flex flex-none items-center gap-1 rounded-card px-1 py-1 text-text-secondary hover:bg-fill-ghost-selected"
        aria-label={alertsLabel}
        onClick={onAlertsClick}
      >
        <IconBell size={18} stroke={1.75} aria-hidden />
        {alertCount > 0 ? (
          <Tag variant="warning" className="px-1.5">
            {alertCount}
          </Tag>
        ) : null}
      </button>

      <ThemeToggle
        preference={themePreference}
        onPreferenceChange={onThemePreferenceChange}
      />

      <div
        className="flex size-[26px] flex-none items-center justify-center rounded-full bg-bg-accent text-[11px] font-medium text-text-accent"
        title={userName ?? userInitials}
      >
        {userInitials}
      </div>
    </header>
  );
}
