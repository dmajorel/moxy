import { render, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { FALLBACK } from "@/lib/format";

import { KeyValue } from "./KeyValue";

/** The row a label belongs to, which is what carries its value. */
function rowOf(label: string): HTMLElement {
  const row = screen.getByText(label).closest("div");
  if (row === null) throw new Error(`no row for ${label}`);
  return row;
}

describe("KeyValue", () => {
  it("renders a definition list, label and value paired", () => {
    const { container } = render(
      <KeyValue
        rows={[
          { label: "Cluster", value: "Qualification" },
          { label: "Noyau", value: "6.14.8-2-pve" },
        ]}
      />,
    );

    expect(container.querySelector("dl")).not.toBeNull();
    expect(container.querySelectorAll("dt")).toHaveLength(2);
    expect(within(rowOf("Cluster")).getByText("Qualification")).toBeInTheDocument();
  });

  // Unknown is never shown as empty: a blank cell reads as "nothing here",
  // which is a different statement from "nobody could tell us".
  it.each([
    ["null", null],
    ["undefined", undefined],
    ["the empty string", ""],
  ])("renders the em dash for %s", (_what, value) => {
    render(<KeyValue rows={[{ label: "IPv4", value }]} />);

    expect(within(rowOf("IPv4")).getByText(FALLBACK)).toBeInTheDocument();
  });

  // A genuine zero is a measurement and stays one.
  it("keeps a zero, which is not an absence", () => {
    render(<KeyValue rows={[{ label: "Mises à jour", value: "0" }]} />);

    const row = rowOf("Mises à jour");
    expect(within(row).getByText("0")).toBeInTheDocument();
    expect(within(row).queryByText(FALLBACK)).toBeNull();
  });

  it("renders a node, not only a string", () => {
    render(
      <KeyValue rows={[{ label: "HA", value: <strong>started</strong> }]} />,
    );

    expect(within(rowOf("HA")).getByText("started").tagName).toBe("STRONG");
  });

  it("uses the monospace face where it is asked for", () => {
    render(
      <KeyValue
        rows={[
          { label: "Noyau", value: "6.14.8-2-pve", mono: true },
          { label: "Cluster", value: "Qualification" },
        ]}
      />,
    );

    expect(within(rowOf("Noyau")).getByText("6.14.8-2-pve").closest("dd")).toHaveClass(
      "font-mono",
    );
    expect(
      within(rowOf("Cluster")).getByText("Qualification").closest("dd"),
    ).not.toHaveClass("font-mono");
  });

  // Two Proxmox tags can share a key — env.prod and env.test both live on a
  // guest someone migrated — so the label is not the row's identity. Keying by
  // it would warn and render unstably.
  it("lists two rows of the same label", () => {
    render(
      <KeyValue
        rows={[
          { label: "env", value: "prod" },
          { label: "env", value: "test" },
        ]}
      />,
    );

    expect(screen.getAllByText("env")).toHaveLength(2);
    expect(screen.getByText("prod")).toBeInTheDocument();
    expect(screen.getByText("test")).toBeInTheDocument();
  });

  // The separator is between rows, not under the list: a trailing hairline
  // would read as a row that failed to render.
  it("draws no rule under the last row", () => {
    const { container } = render(
      <KeyValue
        rows={[
          { label: "a", value: "1" },
          { label: "b", value: "2" },
        ]}
      />,
    );

    const rows = container.querySelectorAll("dl > div");
    expect(rows[0]).toHaveClass("border-b-[0.5px]");
    expect(rows[rows.length - 1]).not.toHaveClass("border-b-[0.5px]");
  });

  it("renders an empty list without failing", () => {
    const { container } = render(<KeyValue rows={[]} />);
    expect(container.querySelectorAll("dl > div")).toHaveLength(0);
  });

  it("merges the className it receives", () => {
    const { container } = render(
      <KeyValue rows={[{ label: "a", value: "1" }]} className="px-3" />,
    );

    expect(container.firstElementChild).toHaveClass("px-3");
    expect(container.firstElementChild).toHaveClass("grid");
  });
});
