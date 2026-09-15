import { act, renderHook } from "@testing-library/react";

import { stubBrokenLocalStorage, stubLanguages } from "@/test/stubs";
import { LANG_ATTRIBUTE, LANG_STORAGE_KEY } from "@/lib/lang";

import { useLang } from "./useLang";

afterEach(() => {
  window.localStorage.clear();
  document.documentElement.removeAttribute(LANG_ATTRIBUTE);
});

describe("useLang", () => {
  it("starts on the browser language when nothing was ever chosen", () => {
    const { result } = renderHook(() => useLang());

    expect(result.current.preference).toBe("system");
    // The suite pins the browser to French; see src/test/setup.ts.
    expect(result.current.locale).toBe("fr");
  });

  it("follows a browser that asks for English", () => {
    const restore = stubLanguages(["en-GB", "fr"]);
    try {
      const { result } = renderHook(() => useLang());

      expect(result.current.preference).toBe("system");
      expect(result.current.locale).toBe("en");
      expect(document.documentElement.getAttribute(LANG_ATTRIBUTE)).toBe("en");
    } finally {
      restore();
    }
  });

  it("restores the choice of a previous visit", () => {
    window.localStorage.setItem(LANG_STORAGE_KEY, "en");

    const { result } = renderHook(() => useLang());

    expect(result.current.preference).toBe("en");
    expect(result.current.locale).toBe("en");
    expect(document.documentElement.getAttribute(LANG_ATTRIBUTE)).toBe("en");
  });

  // An explicit choice wins over the browser, which is the whole point of
  // having a control rather than only a default.
  it("prefers an explicit choice to what the browser asks for", () => {
    const restore = stubLanguages(["en-GB"]);
    window.localStorage.setItem(LANG_STORAGE_KEY, "fr");
    try {
      const { result } = renderHook(() => useLang());

      expect(result.current.locale).toBe("fr");
    } finally {
      restore();
    }
  });

  it("stamps the document and stores the choice when one is made", () => {
    const { result } = renderHook(() => useLang());

    act(() => {
      result.current.setPreference("en");
    });

    expect(result.current.preference).toBe("en");
    expect(result.current.locale).toBe("en");
    expect(document.documentElement.getAttribute(LANG_ATTRIBUTE)).toBe("en");
    expect(window.localStorage.getItem(LANG_STORAGE_KEY)).toBe("en");
  });

  it("goes back to the browser language, leaving nothing stored", () => {
    window.localStorage.setItem(LANG_STORAGE_KEY, "en");
    const { result } = renderHook(() => useLang());

    act(() => {
      result.current.setPreference("system");
    });

    expect(result.current.locale).toBe("fr");
    expect(window.localStorage.getItem(LANG_STORAGE_KEY)).toBeNull();
  });

  it("follows the browser being retuned, while no choice has been made", () => {
    const { result } = renderHook(() => useLang());
    const restore = stubLanguages(["en-GB"]);

    try {
      act(() => {
        window.dispatchEvent(new Event("languagechange"));
      });

      expect(result.current.locale).toBe("en");
      expect(document.documentElement.getAttribute(LANG_ATTRIBUTE)).toBe("en");
    } finally {
      restore();
    }
  });

  it("ignores the browser being retuned once a choice has been made", () => {
    window.localStorage.setItem(LANG_STORAGE_KEY, "fr");
    const { result } = renderHook(() => useLang());
    const restore = stubLanguages(["en-GB"]);

    try {
      act(() => {
        window.dispatchEvent(new Event("languagechange"));
      });

      expect(result.current.locale).toBe("fr");
    } finally {
      restore();
    }
  });

  // Losing the preference is a downgrade to the browser language, never a
  // blank page: the language still applies, it just will not outlive the tab.
  it("still applies the language when the storage refuses the write", () => {
    const restore = stubBrokenLocalStorage();
    try {
      const { result } = renderHook(() => useLang());

      act(() => {
        result.current.setPreference("en");
      });

      expect(result.current.locale).toBe("en");
      expect(document.documentElement.getAttribute(LANG_ATTRIBUTE)).toBe("en");
    } finally {
      restore();
    }
  });
});
