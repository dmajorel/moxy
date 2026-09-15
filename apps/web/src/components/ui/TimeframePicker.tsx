import { useRef } from "react";
import type { KeyboardEvent } from "react";

import type { Timeframe } from "@/api/types";
import { useFormat } from "@/i18n/locale";

/**
 * The window a chart covers: the hour, the day, the week, the month, the year.
 *
 * The five are the ones the RRD routes accept, and the point of offering them
 * is the fixed vertical scale of section 2: because the axis stays [0, 1]
 * whatever the span, an hour and a month are read against the same ruler, so
 * switching between them compares rather than redraws. An auto-fitted chart
 * could not make that claim — each window would invent its own scale.
 *
 * A radio group and not a menu: five options that fit on one line are a choice
 * to be seen, not one to be opened. A menu would also hide which window is on
 * screen behind a click, which is the first thing to know when reading a spike.
 */
export interface TimeframePickerProps {
  value: Timeframe;
  onChange: (timeframe: Timeframe) => void;
  /** Names the group for a screen reader, e.g. "Fenêtre du graphe". */
  label: string;
  className?: string;
}

/** Shortest window first: the list reads as a scale. */
export const TIMEFRAMES: readonly Timeframe[] = [
  "hour",
  "day",
  "week",
  "month",
  "year",
];

const OPTION_CLASSES =
  "rounded-card px-1.5 py-0.5 text-[11px] tabular-nums " +
  "focus-visible:outline-1 focus-visible:outline-accent";

const SELECTED_CLASSES = "bg-fill-ghost-selected text-text-primary";
const UNSELECTED_CLASSES = "text-text-muted hover:text-text-secondary";

export function TimeframePicker({
  value,
  onChange,
  label,
  className,
}: TimeframePickerProps) {
  const { formatTimeframe, formatTimeframeShort } = useFormat();
  const optionRefs = useRef<Array<HTMLButtonElement | null>>([]);

  /**
   * Arrow keys move the selection, which is what a radio group does: the
   * checked option is the one that has focus, so a keyboard user never has to
   * press a second key to commit. Home and End jump to the ends. Tab is left
   * alone — the roving tabindex below makes the group one stop, not five.
   */
  function handleKeyDown(event: KeyboardEvent<HTMLButtonElement>, index: number) {
    const last = TIMEFRAMES.length - 1;
    let next: number;
    switch (event.key) {
      case "ArrowRight":
      case "ArrowDown":
        next = index === last ? 0 : index + 1;
        break;
      case "ArrowLeft":
      case "ArrowUp":
        next = index === 0 ? last : index - 1;
        break;
      case "Home":
        next = 0;
        break;
      case "End":
        next = last;
        break;
      default:
        return;
    }

    event.preventDefault();
    const timeframe = TIMEFRAMES[next];
    if (timeframe === undefined) {
      return;
    }
    onChange(timeframe);
    optionRefs.current[next]?.focus();
  }

  return (
    <div
      role="radiogroup"
      aria-label={label}
      className={["flex items-center gap-0.5", className].filter(Boolean).join(" ")}
    >
      {TIMEFRAMES.map((timeframe, index) => {
        const selected = timeframe === value;
        return (
          <button
            key={timeframe}
            ref={(element) => {
              optionRefs.current[index] = element;
            }}
            type="button"
            role="radio"
            aria-checked={selected}
            // The long form is the accessible name: "24 h" read aloud on its
            // own says nothing about what it selects.
            aria-label={formatTimeframe(timeframe)}
            // One tab stop for the whole group, landing on the current choice.
            tabIndex={selected ? 0 : -1}
            onClick={() => {
              onChange(timeframe);
            }}
            onKeyDown={(event) => {
              handleKeyDown(event, index);
            }}
            className={[
              OPTION_CLASSES,
              selected ? SELECTED_CLASSES : UNSELECTED_CLASSES,
            ].join(" ")}
          >
            {formatTimeframeShort(timeframe)}
          </button>
        );
      })}
    </div>
  );
}
