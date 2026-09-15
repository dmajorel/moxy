import { LOCALES } from "@/lib/lang";

import {
  CATALOGUES_FOR_TEST,
  isMessageKey,
  translate,
  translator,
} from "./messages";

const PLACEHOLDER = /\{(\w+)\}/g;

function placeholdersOf(message: string): Set<string> {
  return new Set([...message.matchAll(PLACEHOLDER)].map((match) => match[1] ?? ""));
}

describe("the catalogues", () => {
  /*
   * Completeness is a COMPILE-time property: `en` is declared as
   * `Record<MessageKey, string>`, so a missing or extra key fails `typecheck`
   * and never reaches here. This asserts it anyway, cheaply, because that
   * declaration is one `as` away from being defeated by someone in a hurry.
   */
  it("hold exactly the same keys", () => {
    const [first, ...rest] = LOCALES.map((locale) =>
      Object.keys(CATALOGUES_FOR_TEST[locale]).sort(),
    );

    for (const keys of rest) {
      expect(keys).toEqual(first);
    }
  });

  it("leave no message empty", () => {
    for (const locale of LOCALES) {
      for (const [key, message] of Object.entries(CATALOGUES_FOR_TEST[locale])) {
        expect(`${locale}/${key}: ${message}`).toMatch(/\S/);
      }
    }
  });

  /*
   * The check the type system genuinely cannot make.
   *
   * A translation that drops `{name}` still type-checks and still renders — it
   * just silently loses the cluster it was naming, and reads as a complete
   * sentence while doing so. That is the one translation mistake nothing else
   * would catch.
   */
  it("carry the same placeholders in every language", () => {
    const french = CATALOGUES_FOR_TEST.fr;

    for (const locale of LOCALES) {
      for (const [key, message] of Object.entries(CATALOGUES_FOR_TEST[locale])) {
        const expected = placeholdersOf(french[key] ?? "");
        expect({ key, locale, names: [...placeholdersOf(message)].sort() }).toEqual({
          key,
          locale,
          names: [...expected].sort(),
        });
      }
    }
  });
});

describe("translate", () => {
  it("returns the message of the language asked for", () => {
    expect(translate("fr", "state.retry")).toBe("Réessayer");
    expect(translate("en", "state.retry")).toBe("Retry");
  });

  it("fills a placeholder in", () => {
    expect(translate("fr", "card.open", { name: "Production" })).toBe(
      "Ouvrir Production",
    );
    expect(translate("en", "card.open", { name: "Production" })).toBe(
      "Open Production",
    );
  });

  it("stringifies a number the way it was given", () => {
    expect(translate("en", "alert.nodeOffline", { count: 2 })).toBe("2 offline");
  });

  it("fills every placeholder of a message, and each occurrence", () => {
    expect(
      translate("fr", "card.chartLabel", {
        name: "Production",
        cpu: "31 %",
        memory: "212 / 256 GiB",
      }),
    ).toBe(
      "Utilisation de Production sur la dernière heure : CPU 31 %, mémoire 212 / 256 GiB",
    );
  });

  /*
   * A placeholder nobody filled is left standing rather than blanked: `{name}`
   * on screen is a bug anyone can see and grep for, while an empty space is a
   * sentence that quietly lost a word.
   */
  it("leaves a placeholder alone rather than blanking it", () => {
    expect(translate("fr", "card.open")).toBe("Ouvrir {name}");
    expect(translate("fr", "card.open", { other: "x" })).toBe("Ouvrir {name}");
  });
});

describe("translator", () => {
  it("binds the locale once, and answers like translate", () => {
    const t = translator("en");

    expect(t("state.retry")).toBe("Retry");
    expect(t("card.open", { name: "Qualification" })).toBe("Open Qualification");
  });
});

describe("isMessageKey", () => {
  /*
   * Its reason to exist: a task type and an HA state are PVE's words, not
   * moxy's, and a newer PVE sends ones this catalogue has never heard of.
   */
  it("recognises a key the catalogue holds", () => {
    expect(isMessageKey("task.type.vzdump")).toBe(true);
    expect(isMessageKey("ha.started")).toBe(true);
  });

  it("rejects anything else, including what Object's prototype carries", () => {
    expect(isMessageKey("task.type.qmfrobnicate")).toBe(false);
    expect(isMessageKey("")).toBe(false);
    expect(isMessageKey("toString")).toBe(false);
    expect(isMessageKey("constructor")).toBe(false);
  });
});
