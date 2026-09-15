// Vite hands both files over as strings; no filesystem path to resolve, and the
// assertions below break the day either one moves rather than passing vacuously.
import indexHtml from "../../index.html?raw";
import boot from "../../public/boot.js?raw";

import { stubBrokenLocalStorage, stubLanguages } from "@/test/stubs";

import {
  applyLang,
  browserLocale,
  isLangPreference,
  isLocale,
  readStoredPreference,
  resolveLocale,
  storePreference,
  watchBrowserLocale,
  DEFAULT_LOCALE,
  LANG_ATTRIBUTE,
  LANG_STORAGE_KEY,
} from "./lang";

afterEach(() => {
  window.localStorage.clear();
  document.documentElement.removeAttribute(LANG_ATTRIBUTE);
});

describe("isLangPreference", () => {
  it("accepts the three preferences", () => {
    expect(isLangPreference("fr")).toBe(true);
    expect(isLangPreference("en")).toBe(true);
    expect(isLangPreference("system")).toBe(true);
  });

  it("rejects anything else", () => {
    expect(isLangPreference("de")).toBe(false);
    expect(isLangPreference("fr-FR")).toBe(false);
    expect(isLangPreference(null)).toBe(false);
    expect(isLangPreference(undefined)).toBe(false);
    expect(isLangPreference(1)).toBe(false);
  });
});

describe("isLocale", () => {
  it("knows the two languages the interface speaks", () => {
    expect(isLocale("fr")).toBe(true);
    expect(isLocale("en")).toBe(true);
    expect(isLocale("system")).toBe(false);
    expect(isLocale("es")).toBe(false);
  });
});

describe("readStoredPreference", () => {
  it("defaults to following the browser", () => {
    expect(readStoredPreference()).toBe("system");
  });

  it("reads back an explicit choice", () => {
    window.localStorage.setItem(LANG_STORAGE_KEY, "en");

    expect(readStoredPreference()).toBe("en");
  });

  it("ignores a value it does not recognise", () => {
    window.localStorage.setItem(LANG_STORAGE_KEY, "klingon");

    expect(readStoredPreference()).toBe("system");
  });

  it("survives a storage that throws", () => {
    const restore = stubBrokenLocalStorage();
    try {
      expect(readStoredPreference()).toBe("system");
    } finally {
      restore();
    }
  });
});

describe("storePreference", () => {
  it("writes an explicit choice", () => {
    expect(storePreference("en")).toBe(true);

    expect(window.localStorage.getItem(LANG_STORAGE_KEY)).toBe("en");
  });

  // The absence of a preference IS "follow the browser": a user who goes back
  // to it leaves nothing behind.
  it("removes the entry rather than writing the word system", () => {
    window.localStorage.setItem(LANG_STORAGE_KEY, "en");

    expect(storePreference("system")).toBe(true);

    expect(window.localStorage.getItem(LANG_STORAGE_KEY)).toBeNull();
  });

  it("reports a storage that refuses the write", () => {
    const restore = stubBrokenLocalStorage();
    try {
      expect(storePreference("fr")).toBe(false);
    } finally {
      restore();
    }
  });
});

describe("browserLocale", () => {
  it("takes the first language it speaks, in the order the user ranked them", () => {
    // A browser set to German first, then English, wants English — not the
    // French further down the list.
    const restore = stubLanguages(["de", "en-GB", "fr"]);
    try {
      expect(browserLocale()).toBe("en");
    } finally {
      restore();
    }
  });

  it("cuts a tag at its first subtag", () => {
    for (const [tag, expected] of [
      ["en-GB", "en"],
      ["en-US", "en"],
      ["EN", "en"],
      ["fr-CH", "fr"],
      ["fr-CA", "fr"],
    ] as const) {
      const restore = stubLanguages([tag]);
      try {
        expect(browserLocale()).toBe(expected);
      } finally {
        restore();
      }
    }
  });

  it("falls back on the source language for anything else", () => {
    const restore = stubLanguages(["de", "es", "ja"]);
    try {
      expect(browserLocale()).toBe(DEFAULT_LOCALE);
      expect(browserLocale()).toBe("fr");
    } finally {
      restore();
    }
  });

  it("falls back on the singular form where the plural is empty", () => {
    const restore = stubLanguages([]);
    try {
      // stubLanguages leaves `language` as "" here, which names nothing.
      expect(browserLocale()).toBe("fr");
    } finally {
      restore();
    }
  });
});

describe("resolveLocale", () => {
  it("returns an explicit choice untouched", () => {
    expect(resolveLocale("en")).toBe("en");
    expect(resolveLocale("fr")).toBe("fr");
  });

  it("asks the browser for the default", () => {
    const restore = stubLanguages(["en-US"]);
    try {
      expect(resolveLocale("system")).toBe("en");
    } finally {
      restore();
    }
  });
});

describe("applyLang", () => {
  it("stamps the locale on the document", () => {
    applyLang("en");

    expect(document.documentElement.getAttribute(LANG_ATTRIBUTE)).toBe("en");
  });

  /*
   * The one place this departs from applyTheme, which REMOVES its attribute for
   * "system" and lets the media query decide. There is no CSS fallback for a
   * language, and an <html> with no lang leaves a screen reader pronouncing a
   * French interface with English phonetics.
   */
  it("always writes an attribute, never removes it", () => {
    applyLang("en");
    applyLang("fr");

    expect(document.documentElement.getAttribute(LANG_ATTRIBUTE)).toBe("fr");
    expect(document.documentElement.hasAttribute(LANG_ATTRIBUTE)).toBe(true);
  });
});

describe("watchBrowserLocale", () => {
  it("reports the new language when the browser is retuned", () => {
    const onChange = vi.fn();
    const stop = watchBrowserLocale(onChange);
    const restore = stubLanguages(["en-GB"]);

    try {
      window.dispatchEvent(new Event("languagechange"));

      expect(onChange).toHaveBeenCalledWith("en");
    } finally {
      restore();
      stop();
    }
  });

  it("stops reporting once unsubscribed", () => {
    const onChange = vi.fn();

    watchBrowserLocale(onChange)();
    window.dispatchEvent(new Event("languagechange"));

    expect(onChange).not.toHaveBeenCalled();
  });
});

/**
 * public/boot.js cannot import this module — it runs before the bundle exists —
 * so it repeats the key, the attribute and the list of locales by hand.
 * Renaming any of them here without the other would leave the page stamped with
 * a language the bundle then disagrees with, which no rendering test catches.
 */
describe("the boot script", () => {
  it("reads the same storage key as this module", () => {
    expect(boot).toContain(`stored("${LANG_STORAGE_KEY}")`);
  });

  it("stamps the same attribute as this module", () => {
    expect(boot).toContain(`setAttribute("${LANG_ATTRIBUTE}", lang)`);
  });

  it("knows the same two locales, and the same fallback", () => {
    expect(boot).toContain(`lang !== "fr" && lang !== "en"`);
    expect(boot).toContain(`primary === "fr" || primary === "en"`);
    expect(boot).toContain(`lang = "${DEFAULT_LOCALE}";`);
  });

  it("reads the browser ranking in order, cutting each tag at its first subtag", () => {
    expect(boot).toContain("navigator.languages");
    expect(boot).toContain('.split("-")[0]');
  });

  // Unlike the theme, which is only stamped when it was explicitly chosen.
  it("always stamps a language, even with nothing stored", () => {
    expect(boot).toMatch(/root\.setAttribute\("lang", lang\);\s*}\)\(\);/);
  });

  it("is the script index.html loads, and the page starts in the source language", () => {
    expect(indexHtml).toContain(`<script src="/boot.js"></script>`);
    // What the document carries until the script runs — and keeps if it is
    // blocked. The source language is the only honest answer there.
    expect(indexHtml).toContain(`<html lang="fr">`);
  });
});
