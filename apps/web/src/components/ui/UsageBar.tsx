/**
 * Fill bar used by the metric cards and the cluster overview.
 *
 * The fill is the neutral data blue, and turns amber past `threshold` — the
 * rule of the screen 4 mock, where memory at 83% is amber while CPU at 31% is
 * not. The ratio is clamped to [0,1] so that a bogus value from the API can
 * never blow the layout open.
 */
import { useFormat } from "@/i18n/locale";

export interface UsageBarProps {
  /** Ratio in [0,1]; anything else (negative, > 1, NaN) is clamped. */
  ratio: number;
  /** Track height in pixels. 3px inside a metric card, 4px on a cluster card. */
  size?: 3 | 4;
  /** Strictly above this the fill turns amber. */
  threshold?: number;
  /** Accessible name, e.g. "Mémoire". */
  label?: string;
  className?: string;
}

const TRACK_HEIGHT: Record<3 | 4, string> = {
  3: "h-[3px]",
  4: "h-[4px]",
};

/** NaN and infinities collapse to 0: unknown is never drawn as full. */
function clampRatio(ratio: number): number {
  if (!Number.isFinite(ratio)) {
    return 0;
  }
  return Math.min(1, Math.max(0, ratio));
}

export function UsageBar({
  ratio,
  size = 3,
  threshold = 0.8,
  label,
  className,
}: UsageBarProps) {
  const { formatRatio } = useFormat();
  const value = clampRatio(ratio);
  const classes = [
    "w-full overflow-hidden rounded-[2px] bg-surface-0",
    TRACK_HEIGHT[size],
    className,
  ]
    .filter(Boolean)
    .join(" ");

  // A ratio that is not a number is not a zero. The bar draws empty because it
  // has to draw something, but it must not ANNOUNCE a figure: omitting
  // aria-valuenow is how the ARIA progressbar says "indeterminate", and
  // reading "0" over an unmeasured node was the same lie as printing 0 %.
  const known = Number.isFinite(ratio);

  return (
    <div
      className={classes}
      role="progressbar"
      aria-valuenow={known ? value : undefined}
      aria-valuemin={0}
      aria-valuemax={1}
      // Without this a screen reader reads the raw 0.83. The percentage is
      // what the sighted reader sees, and format.ts is where it is decided.
      aria-valuetext={known ? formatRatio(value) : undefined}
      aria-label={label}
    >
      <div
        className={[
          "h-full rounded-[2px]",
          value > threshold ? "bg-warning" : "bg-accent",
        ].join(" ")}
        // Rounded to a tenth of a percent: enough for a 4px bar, and it keeps
        // the DOM free of 83.00000000000001% artefacts.
        style={{ width: `${Math.round(value * 1000) / 10}%` }}
      />
    </div>
  );
}
