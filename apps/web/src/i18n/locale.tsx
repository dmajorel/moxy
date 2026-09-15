/**
 * How the display language reaches a component.
 *
 * A context rather than a prop, and that is the one place this codebase departs
 * from the rule it applies to the theme and to the cluster selection. Those are
 * read by one component each — the top bar — so they travel as props from the
 * application root. The language is read by every component that shows a word,
 * which is all of them: threading it through thirty components would turn every
 * signature into a carrier for something none of them decide.
 *
 * WHAT IS IN THE CONTEXT is the resolved locale and nothing else — a string,
 * not an object — so a re-render happens exactly when the language changes.
 * `useT` and `useFormat` look up in tables built once per locale at module
 * load, so both return a stable reference and a component may pass either into
 * a `useCallback` dependency list without churning it.
 *
 * THE DEFAULT IS FRENCH, deliberately, and it is what a component rendered
 * outside any provider gets. That is not a fallback nobody exercises: it is
 * what every component test relies on, and the reason adding a language did not
 * mean touching two hundred assertions.
 */
import { createContext, useContext } from "react";
import type { ReactNode } from "react";

import { translator, type Translator } from "@/i18n/messages";
import { createFormat, type Format } from "@/lib/format";
import { DEFAULT_LOCALE, LOCALES, type Locale } from "@/lib/lang";

const LocaleContext = createContext<Locale>(DEFAULT_LOCALE);

/**
 * One translator and one formatter per locale, built once.
 *
 * Eager rather than memoized in a hook: there are two of them, each is a
 * handful of closures over a lookup table, and building them here is what makes
 * every `useT()` and `useFormat()` in the tree return the same reference for as
 * long as the language does not change.
 */
const TRANSLATORS = Object.fromEntries(
  LOCALES.map((locale) => [locale, translator(locale)]),
) as Record<Locale, Translator>;

const FORMATS = Object.fromEntries(
  LOCALES.map((locale) => [locale, createFormat(locale)]),
) as Record<Locale, Format>;

export interface LocaleProviderProps {
  locale: Locale;
  children: ReactNode;
}

/** Publishes the language the application root resolved, to the whole tree. */
export function LocaleProvider({ locale, children }: LocaleProviderProps) {
  return <LocaleContext value={locale}>{children}</LocaleContext>;
}

/** The language currently displayed. Rarely needed directly. */
export function useLocale(): Locale {
  return useContext(LocaleContext);
}

/** Looks a message up in the current language: `t("tree.empty")`. */
export function useT(): Translator {
  return TRANSLATORS[useContext(LocaleContext)];
}

/**
 * Every formatter whose output depends on the language, already bound to it.
 *
 * Destructure what the component needs — `const { formatBytes } = useFormat()`
 * — which keeps the call sites reading exactly as they did when these were
 * module-level imports.
 */
export function useFormat(): Format {
  return FORMATS[useContext(LocaleContext)];
}
