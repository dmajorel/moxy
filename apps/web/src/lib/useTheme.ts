/**
 * The theme preference as React state, kept in sync with <html> and with
 * localStorage.
 *
 * It is held by the application root rather than by the control that changes
 * it, for the same reason the cluster selection is: the theme belongs to the
 * whole page, and the top bar is a controlled component that owns no
 * application state.
 */
import { useCallback, useEffect, useState } from "react";

import {
  applyTheme,
  readStoredPreference,
  resolveTheme,
  storePreference,
  watchSystemTheme,
  type ResolvedTheme,
  type ThemePreference,
} from "@/lib/theme";

export interface UseThemeResult {
  /** What the user picked; "system" until they pick something. */
  preference: ThemePreference;
  /** What is painted right now, with the system preference resolved. */
  resolvedTheme: ResolvedTheme;
  setPreference: (preference: ThemePreference) => void;
}

export function useTheme(): UseThemeResult {
  // Read once, on the first render. The inline script in index.html has already
  // stamped the attribute from this same key before the first paint; this only
  // brings the value into React.
  const [preference, setPreferenceState] = useState<ThemePreference>(
    readStoredPreference,
  );
  const [resolvedTheme, setResolvedTheme] = useState<ResolvedTheme>(() =>
    resolveTheme(readStoredPreference()),
  );

  // Keeps <html> in step with the state. Running it on mount as well repairs
  // the attribute when the inline script was blocked or absent.
  useEffect(() => {
    applyTheme(preference);
    setResolvedTheme(resolveTheme(preference));
  }, [preference]);

  // Only "system" cares: an explicit choice ignores the desktop switching.
  useEffect(() => {
    if (preference !== "system") {
      return;
    }
    return watchSystemTheme(setResolvedTheme);
  }, [preference]);

  const setPreference = useCallback((next: ThemePreference) => {
    // A storage that refuses the write is not an error the user needs to see:
    // the theme still applies, it just will not outlive the tab. The effect
    // above does the stamping, here and on the first render alike.
    storePreference(next);
    setPreferenceState(next);
  }, []);

  return { preference, resolvedTheme, setPreference };
}
