/**
 * WCAG 2.1 contrast arithmetic.
 *
 * The point of computing this in the repository rather than trusting a colour
 * picker is that the design tokens are the input: `styles/tokens.test.ts` reads
 * `tokens.css` and asserts every text/background pair of both themes, so a
 * token that drifts below the threshold fails the build instead of quietly
 * shipping unreadable 11 px labels.
 *
 * The formulas are the normative ones of WCAG 2.1: relative luminance is the
 * weighted sum of the linearized sRGB channels, and the ratio is
 * `(L_lighter + 0.05) / (L_darker + 0.05)`. Neither is rounded here, because
 * rounding to one decimal is exactly what turns a failing 4.46 into a passing
 * 4.5.
 */

/** Accepts `#rgb` and `#rrggbb`, the only two forms `tokens.css` uses. */
const HEX = /^#(?:[0-9a-f]{3}|[0-9a-f]{6})$/i;

/** sRGB channels of a hex colour, each in [0,1]. */
function channels(colour: string): readonly [number, number, number] {
  if (!HEX.test(colour)) {
    throw new Error(`not a hex colour: ${colour}`);
  }

  const digits = colour.slice(1);
  const wide =
    digits.length === 3
      ? digits
          .split("")
          .map((digit) => digit + digit)
          .join("")
      : digits;
  const packed = parseInt(wide, 16);

  return [
    ((packed >> 16) & 0xff) / 255,
    ((packed >> 8) & 0xff) / 255,
    (packed & 0xff) / 255,
  ];
}

/** The sRGB transfer function, undone: a channel back to linear light. */
function linearize(channel: number): number {
  return channel <= 0.03928
    ? channel / 12.92
    : Math.pow((channel + 0.055) / 1.055, 2.4);
}

/** WCAG 2.1 relative luminance of a hex colour, in [0,1]. */
export function relativeLuminance(colour: string): number {
  const [r, g, b] = channels(colour);

  return 0.2126 * linearize(r) + 0.7152 * linearize(g) + 0.0722 * linearize(b);
}

/**
 * WCAG 2.1 contrast ratio between two hex colours, in [1, 21]. The order of
 * the arguments does not matter: the lighter of the two is found here.
 */
export function contrastRatio(a: string, b: string): number {
  const first = relativeLuminance(a);
  const second = relativeLuminance(b);
  const lighter = Math.max(first, second);
  const darker = Math.min(first, second);

  return (lighter + 0.05) / (darker + 0.05);
}
