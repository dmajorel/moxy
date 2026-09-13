import { IconDeviceDesktop, IconMoon, IconSun } from "@tabler/icons-react";

import type { ThemePreference } from "@/lib/theme";
import { useMenu } from "@/lib/useMenu";

/**
 * The light / dark / system control of the top bar.
 *
 * Three options rather than a two-way switch: "Système" is a real answer, and
 * a control that only toggles cannot express it — once flipped, there is no way
 * back to following the desktop.
 *
 * Like the cluster switcher, it owns nothing but its own open/closed state; the
 * preference is held by the application root and arrives as a prop.
 */
export interface ThemeToggleProps {
  preference: ThemePreference;
  onPreferenceChange: (preference: ThemePreference) => void;
  className?: string;
}

interface Option {
  value: ThemePreference;
  /** Displayed labels are French, sentence case. */
  label: string;
  /** Every Tabler icon shares one component type; any of them names it. */
  icon: typeof IconSun;
}

const LIGHT: Option = { value: "light", label: "Clair", icon: IconSun };
const DARK: Option = { value: "dark", label: "Sombre", icon: IconMoon };
const SYSTEM: Option = {
  value: "system",
  label: "Système",
  icon: IconDeviceDesktop,
};

/** The order they are listed in: the two explicit choices, then the default. */
const OPTIONS: Option[] = [LIGHT, DARK, SYSTEM];

/** Total by construction, so the button never has to guess a fallback. */
const BY_PREFERENCE: Record<ThemePreference, Option> = {
  light: LIGHT,
  dark: DARK,
  system: SYSTEM,
};

const MENU_LABEL = "Thème";

const BUTTON_CLASSES =
  "flex flex-none items-center rounded-card px-1 py-1 text-text-secondary " +
  "hover:bg-fill-ghost-selected focus-visible:outline-1 " +
  "focus-visible:outline-accent";

const ITEM_CLASSES =
  "flex w-full items-center gap-2 rounded-card px-2 py-1.5 text-left " +
  "text-[12px] text-text-secondary hover:bg-fill-ghost-selected " +
  "focus:bg-fill-ghost-selected focus:outline-none";

export function ThemeToggle({
  preference,
  onPreferenceChange,
  className,
}: ThemeToggleProps) {
  const current = BY_PREFERENCE[preference];
  const CurrentIcon = current.icon;

  const menu = useMenu({
    count: OPTIONS.length,
    selectedIndex: OPTIONS.findIndex((option) => option.value === preference),
    onActivate: (index) => {
      const option = OPTIONS[index];
      if (option !== undefined) {
        onPreferenceChange(option.value);
      }
    },
  });

  const classes = ["relative flex-none", className].filter(Boolean).join(" ");

  return (
    // As in AlertsPanel: the wrapper routes the keys of the button and the
    // menu items it holds, and is itself neither focusable nor clickable.
    // eslint-disable-next-line jsx-a11y/no-static-element-interactions -- event delegation
    <div ref={menu.rootRef} className={classes} onKeyDown={menu.onKeyDown}>
      <button
        ref={menu.buttonRef}
        type="button"
        className={BUTTON_CLASSES}
        // The icon alone would say nothing to a screen reader, and the tick in
        // the menu is a colour: the current mode is named in words here.
        aria-label={`${MENU_LABEL} · ${current.label}`}
        aria-haspopup="menu"
        aria-expanded={menu.isOpen}
        onClick={menu.toggle}
      >
        <CurrentIcon size={18} stroke={1.75} aria-hidden />
      </button>

      {menu.isOpen ? (
        <div
          role="menu"
          aria-label={MENU_LABEL}
          className="absolute top-full right-0 z-20 mt-1 min-w-[160px] rounded-card border-[0.5px] border-border bg-surface-0 p-1"
        >
          {OPTIONS.map((option, index) => {
            const isSelected = option.value === preference;
            const OptionIcon = option.icon;
            return (
              <button
                key={option.value}
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
                <OptionIcon size={14} stroke={1.75} aria-hidden />
                {option.label}
              </button>
            );
          })}
        </div>
      ) : null}
    </div>
  );
}
