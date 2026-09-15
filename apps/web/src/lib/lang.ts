/**
 * Language preference: reading it, storing it, and stamping it on the document.
 *
 * The shape is the one of lib/theme.ts, function for function, because it is
 * the same question asked about a different axis: a choice the user can make,
 * a default that defers to the browser, and one attribute on <html> that says
 * what won. Anything that reads oddly here should be compared with the theme
 * before being changed — the two are meant to stay recognisably the same.
 *
 * Two differences are deliberate:
 *
 *   - `applyLang` always writes the attribute, where `applyTheme` removes it
 *     for "system". The theme has a CSS fallback (`prefers-color-scheme`); the
 *     language has none, and an <html> with no lang leaves a screen reader
 *     pronouncing a French interface with English phonetics, or the reverse.
 *     So the RESOLVED locale is stamped, never the preference.
 *   - The default is resolved from `navigator.languages` rather than from a
 *     media query, and is watched through the `languagechange` event.
 *
 * The contract with the document is exactly one attribute on <html>:
 *
 *   lang="fr"   French, chosen or inherited from the browser
 *   lang="en"   English, likewise
 */

/** What the user picked. Persisted; "system" is the default. */
export type LangPreference = "fr" | "en" | "system";

/** What is actually displayed once the browser has been consulted. */
export type Locale = "fr" | "en";

/**
 * Every locale the interface is translated into, in no particular order: the
 * browser's own ordering decides which one wins, not this list.
 */
export const LOCALES: readonly Locale[] = ["fr", "en"];

/**
 * The locale used when the browser asks for anything else.
 *
 * French, because it is the language the interface is written in: the source
 * strings live in the French catalogue and the mockups of section 2 of the
 * handoff are French. An operator who gets the fallback gets a complete
 * interface, never a half-translated one.
 */
export const DEFAULT_LOCALE: Locale = "fr";

/**
 * Namespaced so it cannot collide with anything else on the origin — moxyd
 * serves the frontend and the API from one origin, as the README explains.
 *
 * The boot script in public/boot.js reads this very key before first paint;
 * lang.test.ts asserts the two spellings still match.
 */
export const LANG_STORAGE_KEY = "moxy.lang";

/** The attribute the document carries, and assistive technology reads. */
export const LANG_ATTRIBUTE = "lang";

export function isLangPreference(value: unknown): value is LangPreference {
  return value === "fr" || value === "en" || value === "system";
}

export function isLocale(value: unknown): value is Locale {
  return value === "fr" || value === "en";
}

/**
 * Every access to localStorage goes through this.
 *
 * Reading the property itself throws in a Chrome window where site data are
 * blocked, and Safari's private mode throws on write once the quota is spent,
 * so both the lookup and the call are guarded. Losing the preference is a
 * downgrade to the browser language, never a blank page.
 */
function storage(): Storage | null {
  try {
    return window.localStorage;
  } catch {
    return null;
  }
}

/** Falls back to "system" for anything missing, unreadable or unrecognised. */
export function readStoredPreference(): LangPreference {
  try {
    const stored = storage()?.getItem(LANG_STORAGE_KEY);
    return isLangPreference(stored) ? stored : "system";
  } catch {
    return "system";
  }
}

/**
 * Persists the choice, and reports whether it took. "system" removes the entry
 * instead of writing the word: the absence of a preference *is* "follow the
 * browser", so a user who goes back to it leaves nothing behind.
 */
export function storePreference(preference: LangPreference): boolean {
  const store = storage();
  if (store === null) {
    return false;
  }
  try {
    if (preference === "system") {
      store.removeItem(LANG_STORAGE_KEY);
    } else {
      store.setItem(LANG_STORAGE_KEY, preference);
    }
    return true;
  } catch {
    return false;
  }
}

/**
 * The locale a single BCP 47 tag asks for, or null for one we do not speak.
 *
 * The tag is cut at its first subtag, so `en-GB`, `en-US` and `EN` all answer
 * `en` and `fr-CH` answers `fr`. Matching the whole tag instead would serve a
 * Swiss French browser an English interface for want of an `fr-CH` catalogue,
 * which is the opposite of what it asked for.
 */
function localeOfTag(tag: string): Locale | null {
  const primary = tag.toLowerCase().split("-")[0] ?? "";
  return isLocale(primary) ? primary : null;
}

/**
 * The language the browser asks for, or the default when it asks for none we
 * have.
 *
 * `navigator.languages` is read in the order the user ranked them — a browser
 * set to `["de", "en-GB", "fr"]` wants English, and stopping at the first
 * supported entry is what honours that. `navigator.language` is the fallback
 * for the engines that never shipped the plural form, and jsdom, which fills
 * in neither reliably, lands on the default.
 */
export function browserLocale(): Locale {
  if (typeof navigator === "undefined") {
    return DEFAULT_LOCALE;
  }
  let tags: readonly string[];
  try {
    const { languages, language } = navigator;
    tags = Array.isArray(languages) && languages.length > 0
      ? languages
      : typeof language === "string" && language !== ""
        ? [language]
        : [];
  } catch {
    return DEFAULT_LOCALE;
  }
  for (const tag of tags) {
    if (typeof tag !== "string") continue;
    const locale = localeOfTag(tag);
    if (locale !== null) return locale;
  }
  return DEFAULT_LOCALE;
}

/** What will actually be displayed for a given preference, right now. */
export function resolveLocale(preference: LangPreference): Locale {
  return preference === "system" ? browserLocale() : preference;
}

/**
 * Writes the resolved locale onto <html>.
 *
 * Unlike `applyTheme`, this never removes the attribute: see the note at the
 * top of the file. It takes a locale rather than a preference for the same
 * reason — there is nothing downstream that could resolve "system" later.
 */
export function applyLang(locale: Locale): void {
  if (typeof document === "undefined") {
    return;
  }
  document.documentElement.setAttribute(LANG_ATTRIBUTE, locale);
}

/**
 * Calls back whenever the browser's language ranking changes, and returns the
 * unsubscribe.
 *
 * Only useful while the preference is "system", exactly as `watchSystemTheme`
 * is only useful while the theme preference is: an explicit choice ignores the
 * browser. A no-op unsubscribe is returned where `languagechange` cannot be
 * listened for, so the caller never branches.
 */
export function watchBrowserLocale(onChange: (locale: Locale) => void): () => void {
  if (typeof window === "undefined" || typeof window.addEventListener !== "function") {
    return () => {};
  }
  const listener = () => {
    onChange(browserLocale());
  };
  window.addEventListener("languagechange", listener);
  return () => {
    window.removeEventListener("languagechange", listener);
  };
}
