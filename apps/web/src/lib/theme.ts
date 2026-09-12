/**
 * Theme preference: reading it, storing it, and stamping it on the document.
 *
 * Three states, not two. "light" and "dark" are explicit choices and always win;
 * "system" is the default and defers to `prefers-color-scheme`, which is
 * resolved by the media query in styles/tokens.css rather than here — the CSS
 * needs no JavaScript to paint the right theme, so a slow bundle, a blocked
 * script or a stylesheet loaded on its own still gets it right.
 *
 * The contract with the stylesheet is exactly one attribute on <html>:
 *
 *   data-theme="light"   explicit light, overrides a dark system
 *   data-theme="dark"    explicit dark, overrides a light system
 *   (absent)             follow prefers-color-scheme
 */

/** What the user picked. Persisted; "system" is the default. */
export type ThemePreference = "light" | "dark" | "system";

/** What is actually painted once the system preference has been consulted. */
export type ResolvedTheme = "light" | "dark";

/**
 * Namespaced so it cannot collide with anything else on the origin — moxyd
 * serves the frontend and the API from one origin, as the README explains.
 *
 * The inline script in index.html reads this very key before first paint;
 * theme.test.ts asserts the two spellings still match.
 */
export const THEME_STORAGE_KEY = "moxy.theme";

/** The attribute the stylesheet keys off. */
export const THEME_ATTRIBUTE = "data-theme";

const MEDIA_QUERY = "(prefers-color-scheme: dark)";

export function isThemePreference(value: unknown): value is ThemePreference {
  return value === "light" || value === "dark" || value === "system";
}

/**
 * Every access to localStorage goes through this.
 *
 * Reading the property itself throws in a Chrome window where site data are
 * blocked, and Safari's private mode throws on write once the quota is spent,
 * so both the lookup and the call are guarded. Losing the preference is a
 * downgrade to the system theme, never a blank page.
 */
function storage(): Storage | null {
  try {
    return window.localStorage;
  } catch {
    return null;
  }
}

/** Falls back to "system" for anything missing, unreadable or unrecognised. */
export function readStoredPreference(): ThemePreference {
  try {
    const stored = storage()?.getItem(THEME_STORAGE_KEY);
    return isThemePreference(stored) ? stored : "system";
  } catch {
    return "system";
  }
}

/**
 * Persists the choice, and reports whether it took. "system" removes the entry
 * instead of writing the word: the absence of a preference *is* "follow the
 * system", so a user who goes back to it leaves nothing behind.
 */
export function storePreference(preference: ThemePreference): boolean {
  const store = storage();
  if (store === null) {
    return false;
  }
  try {
    if (preference === "system") {
      store.removeItem(THEME_STORAGE_KEY);
    } else {
      store.setItem(THEME_STORAGE_KEY, preference);
    }
    return true;
  } catch {
    return false;
  }
}

/**
 * The system preference, or "light" when it cannot be asked — matchMedia is
 * missing from jsdom and from older embedded browsers, and an unanswerable
 * question is not a vote for dark.
 */
export function systemTheme(): ResolvedTheme {
  if (typeof window === "undefined" || typeof window.matchMedia !== "function") {
    return "light";
  }
  try {
    return window.matchMedia(MEDIA_QUERY).matches ? "dark" : "light";
  } catch {
    return "light";
  }
}

/** What will actually be painted for a given preference, right now. */
export function resolveTheme(preference: ThemePreference): ResolvedTheme {
  return preference === "system" ? systemTheme() : preference;
}

/**
 * Writes the preference onto <html>, where the stylesheet reads it.
 *
 * "system" removes the attribute rather than writing the resolved theme, so the
 * media query keeps ownership of that case and a desktop switched to dark at
 * three in the morning is followed without the page being told.
 */
export function applyTheme(preference: ThemePreference): void {
  if (typeof document === "undefined") {
    return;
  }
  const root = document.documentElement;
  if (preference === "system") {
    root.removeAttribute(THEME_ATTRIBUTE);
  } else {
    root.setAttribute(THEME_ATTRIBUTE, preference);
  }
}

/**
 * Calls back whenever the system flips, and returns the unsubscribe.
 *
 * Only useful while the preference is "system": the CSS repaints on its own,
 * but the control has to relabel itself. A no-op unsubscribe is returned when
 * matchMedia is unavailable, so the caller never branches.
 */
export function watchSystemTheme(onChange: (theme: ResolvedTheme) => void): () => void {
  if (typeof window === "undefined" || typeof window.matchMedia !== "function") {
    return () => {};
  }

  let query: MediaQueryList;
  try {
    query = window.matchMedia(MEDIA_QUERY);
  } catch {
    return () => {};
  }

  // addEventListener is the modern spelling; Safari below 14 only has
  // addListener, and it is still the one some embedded WebViews ship.
  const listener = (event: MediaQueryListEvent) => {
    onChange(event.matches ? "dark" : "light");
  };
  if (typeof query.addEventListener === "function") {
    query.addEventListener("change", listener);
    return () => {
      query.removeEventListener("change", listener);
    };
  }
  if (typeof query.addListener === "function") {
    query.addListener(listener);
    return () => {
      query.removeListener(listener);
    };
  }
  return () => {};
}
