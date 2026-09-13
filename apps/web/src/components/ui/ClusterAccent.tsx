import type { CSSProperties } from "react";

import type { Unknown } from "@/api/types";

/**
 * The accent colour a cluster declares in the server configuration.
 *
 * It is the one colour of this interface that is not a design token: it comes
 * from `clusters[].color`, is validated as `#rrggbb` by the backend and is
 * passed through untouched, so that an operator running a dozen clusters whose
 * names all read alike can tell production from qualification at a glance.
 *
 * Two consequences, both deliberate:
 *
 *  - The value reaches the DOM as a CUSTOM PROPERTY, never as a Tailwind class
 *    built by concatenation. `bg-red-500` assembled from a string is a class
 *    Tailwind's extractor cannot see and therefore never generates, so such a
 *    dot would be invisible in production and painted in development only.
 *    Here the utility is a literal the extractor reads — the runtime supplies
 *    the value alone, and everything else about the mark (its size, its radius,
 *    its position) stays in the stylesheet where the rest of the design lives.
 *    This file is the single exemption from the no-inline-style rule, declared
 *    in eslint.config.js next to the three geometry files.
 *
 *  - The mark is DECORATIVE. The colour says nothing the cluster name next to
 *    it does not already say, so it is `aria-hidden` rather than an image with
 *    a name: announcing "carré violet" before every cluster name would be noise
 *    in the one panel that is always on screen. Nothing is carried by colour
 *    alone, which is what section 2 requires.
 *
 * It is a rounded SQUARE, not a circle, because it sits beside the 7px round
 * StatusDot: two dots of the same shape side by side would read as two states
 * and the operator would look for the meaning of the second one.
 */

/**
 * The shape the backend guarantees (`colorPattern` in internal/config).
 *
 * Checked again here rather than trusted: every payload this UI renders comes
 * through an `as unknown as` cast, so what TypeScript believes about `color` is
 * what the contract claims, not what the bytes contain — and this particular
 * field is the only one that ends up inside a style attribute. An unexpected
 * value renders nothing, exactly as an absent one does.
 */
const HEX_COLOUR = /^#[0-9a-fA-F]{6}$/;

export interface ClusterAccentProps {
  /** null — or absent — when the cluster declares no accent: nothing is drawn. */
  color?: Unknown<string>;
  className?: string;
}

const BASE_CLASSES =
  "inline-block size-2 flex-none rounded-[2px] bg-[var(--cluster-accent)]";

export function ClusterAccent({ color, className }: ClusterAccentProps) {
  if (color === null || color === undefined || !HEX_COLOUR.test(color)) {
    return null;
  }

  const classes = [BASE_CLASSES, className].filter(Boolean).join(" ");
  // The cast is what React asks for to set a custom property: CSSProperties
  // knows the CSS properties it ships with, and `--cluster-accent` is ours.
  const style = { "--cluster-accent": color } as CSSProperties;

  return <span aria-hidden className={classes} style={style} />;
}
