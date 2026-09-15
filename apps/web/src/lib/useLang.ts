/**
 * The language preference as React state, kept in sync with <html> and with
 * localStorage.
 *
 * It is held by the application root rather than by the control that changes
 * it, for the same reason the theme and the cluster selection are: the language
 * belongs to the whole page, and the top bar is a controlled component that
 * owns no application state. The resolved locale is then handed to the tree
 * through the provider of i18n/locale.tsx.
 */
import { useCallback, useEffect, useState } from "react";

import {
  applyLang,
  readStoredPreference,
  resolveLocale,
  storePreference,
  watchBrowserLocale,
  type LangPreference,
  type Locale,
} from "@/lib/lang";

export interface UseLangResult {
  /** What the user picked; "system" until they pick something. */
  preference: LangPreference;
  /** What is displayed right now, with the browser preference resolved. */
  locale: Locale;
  setPreference: (preference: LangPreference) => void;
}

export function useLang(): UseLangResult {
  // Read once, on the first render. The boot script in index.html has already
  // stamped the attribute from this same key before the first paint; this only
  // brings the value into React.
  const [preference, setPreferenceState] = useState<LangPreference>(
    readStoredPreference,
  );
  const [locale, setLocale] = useState<Locale>(() =>
    resolveLocale(readStoredPreference()),
  );

  // Keeps <html> in step with the state. Running it on mount as well repairs
  // the attribute when the boot script was blocked or absent.
  useEffect(() => {
    const next = resolveLocale(preference);
    setLocale(next);
    applyLang(next);
  }, [preference]);

  // Only "system" cares: an explicit choice ignores the browser being retuned.
  useEffect(() => {
    if (preference !== "system") {
      return;
    }
    return watchBrowserLocale((next) => {
      setLocale(next);
      applyLang(next);
    });
  }, [preference]);

  const setPreference = useCallback((next: LangPreference) => {
    // A storage that refuses the write is not an error the user needs to see:
    // the language still applies, it just will not outlive the tab. The effect
    // above does the stamping, here and on the first render alike.
    storePreference(next);
    setPreferenceState(next);
  }, []);

  return { preference, locale, setPreference };
}
