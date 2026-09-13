import { describe, expect, it } from "vitest";

import { contrastRatio, relativeLuminance } from "./contrast";

/**
 * The arithmetic `styles/tokens.test.ts` leans on to judge the palette.
 *
 * Nothing here is rounded: rounding to one decimal is exactly what turns a
 * failing 4.46 into a passing 4.5, so the checks are against the normative
 * WCAG 2.1 values with the tolerance stated.
 */

describe("relativeLuminance", () => {
  it("computes the bounds of sRGB", () => {
    expect(relativeLuminance("#000000")).toBe(0);
    expect(relativeLuminance("#ffffff")).toBe(1);
  });

  // The weights are not equal: the eye is far more sensitive to green, which
  // is why a "mid grey" and a "mid green" are not interchangeable.
  it("weighs the channels as WCAG does", () => {
    expect(relativeLuminance("#ff0000")).toBeCloseTo(0.2126, 10);
    expect(relativeLuminance("#00ff00")).toBeCloseTo(0.7152, 10);
    expect(relativeLuminance("#0000ff")).toBeCloseTo(0.0722, 10);
  });

  // Below 0.03928 the transfer function is linear, above it a power curve:
  // a very dark token lands on one side or the other of that knee.
  it("takes both branches of the transfer function", () => {
    // 5/255 = 0.0196, under the knee: a plain division by 12.92.
    expect(relativeLuminance("#050505")).toBeCloseTo(5 / 255 / 12.92, 10);
    // 12/255 = 0.047, over it: the power curve.
    expect(relativeLuminance("#0c0c0c")).toBeCloseTo(
      Math.pow((12 / 255 + 0.055) / 1.055, 2.4),
      10,
    );
  });

  it("expands the three-digit form", () => {
    expect(relativeLuminance("#fff")).toBe(relativeLuminance("#ffffff"));
    expect(relativeLuminance("#F00")).toBeCloseTo(0.2126, 10);
  });

  // A renamed token read from the stylesheet arrives as something that is not
  // a colour; failing loudly is what keeps the palette test from passing on
  // nonsense.
  it.each(["rgb(0 0 0)", "#ff", "#12345", "white", "", "#gggggg"])(
    "rejects %s",
    (value) => {
      expect(() => relativeLuminance(value)).toThrow(/not a hex colour/);
    },
  );
});

describe("contrastRatio", () => {
  it("computes the extremes", () => {
    expect(contrastRatio("#000000", "#ffffff")).toBeCloseTo(21, 10);
    expect(contrastRatio("#7f8694", "#7f8694")).toBe(1);
  });

  // Contrast is a property of a pair, not of a foreground: the lighter of the
  // two is found here, so a caller never has to order its arguments.
  it("does not care which colour comes first", () => {
    expect(contrastRatio("#ffffff", "#000000")).toBe(
      contrastRatio("#000000", "#ffffff"),
    );
    expect(contrastRatio("#1d9e75", "#0f1115")).toBe(
      contrastRatio("#0f1115", "#1d9e75"),
    );
  });

  it("stays within the range WCAG defines", () => {
    for (const pair of [
      ["#7f8694", "#0f1115"],
      ["#1d9e75", "#ffffff"],
      ["#d85a30", "#f7f8fa"],
    ] as const) {
      const value = contrastRatio(pair[0], pair[1]);
      expect(value).toBeGreaterThanOrEqual(1);
      expect(value).toBeLessThanOrEqual(21);
    }
  });

  it("expands the three-digit form", () => {
    expect(contrastRatio("#fff", "#000")).toBeCloseTo(21, 10);
  });
});
