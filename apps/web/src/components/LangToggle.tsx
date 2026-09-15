import { IconLanguage } from "@tabler/icons-react";

import { useT } from "@/i18n/locale";
import type { LangPreference } from "@/lib/lang";
import { useMenu } from "@/lib/useMenu";

/**
 * The French / English / browser control of the top bar.
 *
 * Deliberately the same control as ThemeToggle, down to the menu mechanics:
 * three options rather than a two-way switch, because "follow the browser" is a
 * real answer and a toggle cannot express it — once flipped, there would be no
 * way back to it.
 *
 * Like the theme control, it owns nothing but its own open/closed state; the
 * preference is held by the application root and arrives as a prop.
 *
 * THE TWO LANGUAGE NAMES ARE NOT TRANSLATED. "Français" stays "Français" in an
 * English menu and "English" stays "English" in a French one, which is the
 * convention every language picker follows and the only thing that lets someone
 * find their own language in an interface they cannot read. Only the third
 * entry, which names a behaviour rather than a language, follows the interface.
 */
export interface LangToggleProps {
  preference: LangPreference;
  onPreferenceChange: (preference: LangPreference) => void;
  className?: string;
}

interface Option {
  value: LangPreference;
  /**
   * Written out here rather than looked up: an endonym is the same string in
   * every catalogue, so putting it in one would mean maintaining two copies
   * that must never diverge.
   */
  label: string | null;
}

const FRENCH: Option = { value: "fr", label: "Français" };
const ENGLISH: Option = { value: "en", label: "English" };
/** The one entry that follows the interface: it names a behaviour. */
const BROWSER: Option = { value: "system", label: null };

/** The order they are listed in: the two explicit choices, then the default. */
const OPTIONS: Option[] = [FRENCH, ENGLISH, BROWSER];

/** Total by construction, so the button never has to guess a fallback. */
const BY_PREFERENCE: Record<LangPreference, Option> = {
  fr: FRENCH,
  en: ENGLISH,
  system: BROWSER,
};

const BUTTON_CLASSES =
  "flex flex-none items-center rounded-card px-1 py-1 text-text-secondary " +
  "hover:bg-fill-ghost-selected focus-visible:outline-1 " +
  "focus-visible:outline-accent";

const ITEM_CLASSES =
  "flex w-full items-center gap-2 rounded-card px-2 py-1.5 text-left " +
  "text-[12px] text-text-secondary hover:bg-fill-ghost-selected " +
  "focus:bg-fill-ghost-selected focus:outline-none";

export function LangToggle({
  preference,
  onPreferenceChange,
  className,
}: LangToggleProps) {
  const t = useT();
  const menuLabel = t("lang.label");
  /** The endonym, or the translated wording of "follow the browser". */
  const labelOf = (option: Option) => option.label ?? t("lang.system");

  const menu = useMenu({
    count: OPTIONS.length,
    // Opening the menu lands on the language already in force.
    initialIndex: () => OPTIONS.findIndex((option) => option.value === preference),
    onActivate: (index) => {
      const option = OPTIONS[index];
      if (option !== undefined) {
        onPreferenceChange(option.value);
      }
    },
  });

  const classes = ["relative flex-none", className].filter(Boolean).join(" ");

  return (
    // As in ThemeToggle: the wrapper routes the keys of the button and the
    // menu items it holds, and is itself neither focusable nor clickable.
    // eslint-disable-next-line jsx-a11y/no-static-element-interactions -- event delegation
    <div ref={menu.rootRef} className={classes} onKeyDown={menu.onKeyDown}>
      <button
        {...menu.buttonProps}
        type="button"
        className={BUTTON_CLASSES}
        // The glyph alone would say nothing to a screen reader, and the tick in
        // the menu is a colour: the current choice is named in words here.
        aria-label={`${menuLabel} · ${labelOf(BY_PREFERENCE[preference])}`}
      >
        <IconLanguage size={18} stroke={1.75} aria-hidden />
      </button>

      {menu.isOpen ? (
        <div
          role="menu"
          aria-label={menuLabel}
          className="absolute top-full right-0 z-20 mt-1 min-w-[160px] rounded-card border-[0.5px] border-border bg-surface-0 p-1"
        >
          {OPTIONS.map((option, index) => {
            const isSelected = option.value === preference;
            return (
              <button
                key={option.value}
                ref={menu.itemRef(index)}
                type="button"
                role="menuitemradio"
                aria-checked={isSelected}
                tabIndex={-1}
                // An endonym is not in the language of the page around it, so
                // it carries its own: without this a screen reader reads
                // "English" with French phonetics, and "Français" with
                // English ones.
                lang={option.label === null ? undefined : option.value}
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
                {labelOf(option)}
              </button>
            );
          })}
        </div>
      ) : null}
    </div>
  );
}
