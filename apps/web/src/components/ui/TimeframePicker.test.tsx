import { fireEvent, render, screen } from "@testing-library/react";
import { useState } from "react";

import type { Timeframe } from "@/api/types";

import { TIMEFRAMES, TimeframePicker } from "./TimeframePicker";

/**
 * The picker is controlled, so a keyboard test needs something to hold the
 * value: without it every arrow press would move from the same option and the
 * roving tabindex would never advance.
 */
function Harness({ initial = "hour" as Timeframe }) {
  const [value, setValue] = useState<Timeframe>(initial);
  return <TimeframePicker value={value} onChange={setValue} label="Fenêtre du graphe" />;
}

describe("TimeframePicker", () => {
  it("offers the five windows the RRD routes accept", () => {
    render(<TimeframePicker value="hour" onChange={vi.fn()} label="Fenêtre du graphe" />);

    const group = screen.getByRole("radiogroup", { name: "Fenêtre du graphe" });
    expect(group).toBeInTheDocument();
    expect(screen.getAllByRole("radio")).toHaveLength(TIMEFRAMES.length);
    expect(screen.getByRole("radio", { name: "Dernières 24 h" })).toHaveTextContent(
      "24 h",
    );
  });

  // The window on screen has to be readable without clicking anything, and by
  // something other than a shade of grey.
  it("marks the current window as checked", () => {
    render(<TimeframePicker value="week" onChange={vi.fn()} label="Fenêtre du graphe" />);

    expect(screen.getByRole("radio", { name: "7 derniers jours" })).toBeChecked();
    expect(screen.getByRole("radio", { name: "Dernière heure" })).not.toBeChecked();
  });

  it("reports the window a click asks for", () => {
    const onChange = vi.fn();
    render(<TimeframePicker value="hour" onChange={onChange} label="Fenêtre du graphe" />);

    fireEvent.click(screen.getByRole("radio", { name: "30 derniers jours" }));

    expect(onChange).toHaveBeenCalledWith("month");
  });

  // A radio group is ONE tab stop: five stops would make the chart legend a
  // detour on the way to the rest of the page.
  it("keeps a single tab stop, on the current window", () => {
    render(<TimeframePicker value="day" onChange={vi.fn()} label="Fenêtre du graphe" />);

    expect(screen.getByRole("radio", { name: "Dernières 24 h" })).toHaveAttribute(
      "tabindex",
      "0",
    );
    expect(screen.getByRole("radio", { name: "Dernière heure" })).toHaveAttribute(
      "tabindex",
      "-1",
    );
  });

  // Selection follows focus, as a radio group does: an operator never has to
  // press a second key to commit the window they just moved onto.
  it("moves the selection with the arrow keys", () => {
    render(<Harness />);

    const hour = screen.getByRole("radio", { name: "Dernière heure" });
    hour.focus();
    fireEvent.keyDown(hour, { key: "ArrowRight" });

    const day = screen.getByRole("radio", { name: "Dernières 24 h" });
    expect(day).toBeChecked();
    expect(day).toHaveFocus();

    fireEvent.keyDown(day, { key: "ArrowLeft" });
    expect(screen.getByRole("radio", { name: "Dernière heure" })).toBeChecked();
  });

  // Wrapping rather than stopping: five options are a ring short enough that
  // the far end is one key away from either edge.
  it("wraps at both ends and jumps with Home and End", () => {
    render(<Harness />);

    const hour = screen.getByRole("radio", { name: "Dernière heure" });
    hour.focus();
    fireEvent.keyDown(hour, { key: "ArrowLeft" });
    expect(screen.getByRole("radio", { name: "Dernière année" })).toBeChecked();

    fireEvent.keyDown(screen.getByRole("radio", { name: "Dernière année" }), {
      key: "ArrowRight",
    });
    expect(screen.getByRole("radio", { name: "Dernière heure" })).toBeChecked();

    fireEvent.keyDown(screen.getByRole("radio", { name: "Dernière heure" }), {
      key: "End",
    });
    expect(screen.getByRole("radio", { name: "Dernière année" })).toBeChecked();

    fireEvent.keyDown(screen.getByRole("radio", { name: "Dernière année" }), {
      key: "Home",
    });
    expect(screen.getByRole("radio", { name: "Dernière heure" })).toBeChecked();
  });

  it("leaves every other key to the page", () => {
    const onChange = vi.fn();
    render(<TimeframePicker value="hour" onChange={onChange} label="Fenêtre du graphe" />);

    fireEvent.keyDown(screen.getByRole("radio", { name: "Dernière heure" }), {
      key: "Tab",
    });

    expect(onChange).not.toHaveBeenCalled();
  });
});
