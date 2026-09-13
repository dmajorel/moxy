export interface LogoProps {
  /** Rendered size in pixels, applied to both sides. */
  size?: number;
  /**
   * Alternative text. Left out by default: the mark sits next to the "moxy"
   * wordmark in the application bar, where announcing it a second time would
   * only add noise. Pass a label wherever it stands on its own.
   */
  title?: string;
  className?: string;
}

/**
 * The mark: an X built from four 45° bars, two per diagonal, kept apart by two
 * hairline gaps that cross at the centre. Every arm ends on a flat horizontal
 * cut, which is what keeps the silhouette from reading as a rotated square.
 *
 * The geometry is written out as a single path in a 0 0 32 32 viewBox rather
 * than drawn with strokes: a stroked X would thicken with the size and blur its
 * ends, while a filled outline scales cleanly down to the 22px of the
 * application bar, which is the only size that matters today.
 *
 * The gaps are holes, not white strokes — the page shows through them — so the
 * mark sits on any surface, light or dark, without carrying its own background.
 */
const PATH = [
  // Top chevron.
  "M3.15 1.4H9.75L16 7.65L22.25 1.4H28.85L16 14.25Z",
  // Bottom chevron.
  "M2.25 30.6H8.85L16 23.45L23.15 30.6H29.75L16 16.85Z",
  // Left chevron; the two outer corners are cut by the edge of the viewBox.
  "M4.55 5.4H0V7.45L8.1 15.55L0 23.65V26.6H3.65L14.7 15.55Z",
  // Right chevron, mirrored.
  "M27.45 5.4H32V7.45L23.9 15.55L32 23.65V26.6H28.35L17.3 15.55Z",
].join("");

const BASE_CLASSES = "flex-none text-brand";

export function Logo({ size = 22, title, className }: LogoProps) {
  const classes = [BASE_CLASSES, className].filter(Boolean).join(" ");

  return (
    <svg
      className={classes}
      width={size}
      height={size}
      viewBox="0 0 32 32"
      fill="currentColor"
      role={title === undefined ? undefined : "img"}
      aria-label={title}
      aria-hidden={title === undefined ? true : undefined}
    >
      <path d={PATH} />
    </svg>
  );
}
