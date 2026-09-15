import { readFileSync } from "node:fs";
import { resolve } from "node:path";

import { describe, expect, it } from "vitest";

import { contrastRatio } from "@/lib/contrast";

/*
 * The stylesheet is read from disk rather than imported: Vitest runs with the
 * CSS pipeline disabled, so `./tokens.css?raw` hands back an empty string and
 * every assertion below would pass vacuously. Reading the very file the bundle
 * ships also means the tokens under test cannot drift from the ones in use.
 * The path is resolved from the Vitest root, which is `apps/web`.
 */
const tokensCss = readFileSync(
  resolve(process.cwd(), "src/styles/tokens.css"),
  "utf8",
);

/**
 * Contrast guarantee of the two themes.
 *
 * Section 2 fixes the semantic colours, not the neutrals, and the neutrals are
 * where readability is won or lost: `--text-muted` carries the whole second
 * level of information — breadcrumbs, column headers, "Dernière heure", read
 * freshness — at 11 px, which is text the AA threshold treats as small, hence
 * 4.5:1 rather than 3:1.
 *
 * Every declaration is read from `tokens.css` rather than restated here: a
 * token lowered below its threshold has to fail this test, which cannot happen
 * if the expected value lives in the assertion.
 */

/** `--name: #hex;`, the only shape a colour token takes in `tokens.css`. */
const DECLARATION = /--([a-z0-9-]+):\s*(#[0-9a-f]{3,6})\s*;/gi;

type Palette = Readonly<Record<string, string>>;

/**
 * The light palette is the literal one; the dark palette is the same names
 * overridden by their `--dark-*` counterparts, which is precisely what the
 * media query and the `[data-theme="dark"]` rule do at runtime.
 */
function palettes(): { light: Palette; dark: Palette } {
  const light: Record<string, string> = {};
  const overrides: Record<string, string> = {};

  for (const match of tokensCss.matchAll(DECLARATION)) {
    const name = match[1];
    const value = match[2];
    if (name === undefined || value === undefined) {
      continue;
    }

    if (name.startsWith("dark-")) {
      overrides[name.slice("dark-".length)] = value.toLowerCase();
    } else {
      light[name] = value.toLowerCase();
    }
  }

  return { light, dark: { ...light, ...overrides } };
}

const { light, dark } = palettes();

const THEMES: ReadonlyArray<readonly [string, Palette]> = [
  ["light", light],
  ["dark", dark],
];

/** The three surfaces a component may paint behind text. */
const SURFACES = ["surface-0", "surface-1", "surface-2"] as const;

/**
 * Text tokens, each of which may sit on any of the three surfaces.
 *
 * `--text-success` and `--text-warning-strong` are there because the sidebar
 * tree paints its cluster glyph and its maintenance wrench with them, straight
 * on the surface: a 1.75px stroke is read, not glanced at, so it is held to
 * the text threshold rather than to the 3:1 of a flat status fill.
 */
const TEXT_ON_SURFACES = [
  "text-primary",
  "text-secondary",
  "text-muted",
  "text-success",
  "text-warning-strong",
  // The guest glyph of the tree adds these two, for the same reason: a 1.75px
  // stroke is read, not glanced at. --text-danger had only ever been used on
  // --bg-danger, and --text-info exists for this and nothing else.
  "text-danger",
  "text-info",
] as const;

/** Text tokens that sit on the status fill of the same name. */
const TEXT_ON_FILL = [
  ["text-success", "bg-success"],
  ["text-warning", "bg-warning"],
  ["text-warning-strong", "bg-warning"],
  ["text-accent", "bg-accent"],
] as const;

/**
 * Status fills are graphics — a bar, a dot, a sparkline — so 3:1 applies, not
 * 4.5:1, and each of them always carries a textual equivalent besides.
 */
const FILLS = ["success", "accent", "brand"] as const;

const AA_TEXT = 4.5;
const AA_GRAPHIC = 3;

/** Two decimals, the way the README and the issue state these ratios. */
function ratio(palette: Palette, a: string, b: string): number {
  const foreground = palette[a];
  const background = palette[b];

  // A renamed or deleted token would otherwise make every assertion below pass
  // vacuously on `undefined`.
  if (foreground === undefined || background === undefined) {
    throw new Error(`missing token --${foreground === undefined ? a : b}`);
  }

  return Math.round(contrastRatio(foreground, background) * 100) / 100;
}

describe("token palettes", () => {
  it("reads both themes from the stylesheet", () => {
    // A stylesheet read as an empty string would make the whole file pass on
    // nothing at all, which is the one failure mode this test cannot report.
    expect(tokensCss.length).toBeGreaterThan(0);
    expect(Object.keys(light).length).toBeGreaterThan(15);
    expect(light["surface-0"]).toBe("#ffffff");
    // The dark theme is a second set of values behind the same names.
    expect(dark["surface-0"]).not.toBe(light["surface-0"]);
    expect(Object.keys(dark)).toEqual(Object.keys(light));
  });
});

describe.each(THEMES)("%s theme", (theme, palette) => {
  it.each(TEXT_ON_SURFACES)("reads %s on every surface", (text) => {
    for (const surface of SURFACES) {
      expect(
        ratio(palette, text, surface),
        `--${text} on --${surface} (${theme})`,
      ).toBeGreaterThanOrEqual(AA_TEXT);
    }
  });

  it.each(TEXT_ON_FILL)("reads %s on %s", (text, fill) => {
    expect(
      ratio(palette, text, fill),
      `--${text} on --${fill} (${theme})`,
    ).toBeGreaterThanOrEqual(AA_TEXT);
  });

  it("reads text on the hover fill", () => {
    // `--fill-ghost-selected` is a hover fill on rows and menu entries, whose
    // text is primary or secondary. The one muted thing it ever hosts is the
    // close icon of the maintenance dialog — a graphic, hence 3:1.
    for (const text of ["text-primary", "text-secondary"] as const) {
      expect(
        ratio(palette, text, "fill-ghost-selected"),
        `--${text} on --fill-ghost-selected (${theme})`,
      ).toBeGreaterThanOrEqual(AA_TEXT);
    }

    expect(
      ratio(palette, "text-muted", "fill-ghost-selected"),
      `--text-muted on --fill-ghost-selected (${theme})`,
    ).toBeGreaterThanOrEqual(AA_GRAPHIC);
  });

  it.each(FILLS)("shows the %s fill on every surface", (fill) => {
    for (const surface of SURFACES) {
      expect(
        ratio(palette, fill, surface),
        `--${fill} on --${surface} (${theme})`,
      ).toBeGreaterThanOrEqual(AA_GRAPHIC);
    }
  });
});

describe("known exception: the amber of section 2", () => {
  /**
   * `--warning: #EF9F27` is imposed by section 2 and is the one fill the light
   * theme cannot bring to 3:1 — it is asserted here so that the exception is
   * recorded rather than forgotten. It is only ever a fill (bar, dot) and the
   * state it signals is always written out in words next to it.
   */
  it("stays below 3:1 on the light surfaces and above on the dark ones", () => {
    for (const surface of SURFACES) {
      expect(ratio(light, "warning", surface)).toBeLessThan(AA_GRAPHIC);
      expect(ratio(dark, "warning", surface)).toBeGreaterThanOrEqual(
        AA_GRAPHIC,
      );
    }
  });
});
