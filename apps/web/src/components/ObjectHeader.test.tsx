import { render, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { ObjectHeader } from "./ObjectHeader";

function header(): HTMLElement {
  const element = screen.getByRole("banner");
  return element;
}

describe("ObjectHeader", () => {
  // Section 2 puts the state on the same line as the name and never in a
  // key/value list below: an operator opening a VM should know whether it is
  // running before reading anything else.
  it("puts the name and the state on one line", () => {
    render(
      <ObjectHeader
        breadcrumb={["Qualification", "Nœud"]}
        name="prox-qual-2201-cit"
        status="online"
        stateLabel="En ligne · 41 j"
      />,
    );

    expect(
      screen.getByRole("heading", { level: 1, name: "prox-qual-2201-cit" }),
    ).toBeInTheDocument();
    expect(within(header()).getByText("En ligne · 41 j")).toBeInTheDocument();
    expect(within(header()).getByText("Qualification › Nœud")).toBeInTheDocument();
  });

  it("omits the breadcrumb line when there is no path", () => {
    render(
      <ObjectHeader breadcrumb={[]} name="pve-1" status="online" stateLabel="En ligne" />,
    );

    expect(header().textContent).toBe("pve-1En ligne");
  });

  // The tone is a summary of the status, and the three groups are not
  // arbitrary: something being drained or degraded is amber because it asks
  // for attention, something running is green, and everything else is neutral
  // rather than alarming.
  it.each([
    ["online", "bg-bg-success"],
    ["running", "bg-bg-success"],
    ["healthy", "bg-bg-success"],
    ["maintenance", "bg-bg-warning"],
    ["degraded", "bg-bg-warning"],
    ["offline", "bg-surface-2"],
    ["stopped", "bg-surface-2"],
    ["unreachable", "bg-surface-2"],
  ] as const)("tints the state tag for %s", (status, expected) => {
    render(
      <ObjectHeader breadcrumb={[]} name="x" status={status} stateLabel="état" />,
    );

    expect(screen.getByText("état").closest("span")).toHaveClass(expected);
  });

  it("renders the chips it is given, in order", () => {
    render(
      <ObjectHeader
        breadcrumb={[]}
        name="pve-1"
        status="online"
        stateLabel="En ligne"
        chips={["PVE 9.2.11", "3 invités"]}
      />,
    );

    expect(screen.getByText("PVE 9.2.11")).toBeInTheDocument();
    expect(screen.getByText("3 invités")).toBeInTheDocument();
  });

  it("draws no action area when there is no action", () => {
    const { container } = render(
      <ObjectHeader breadcrumb={[]} name="pve-1" status="online" stateLabel="En ligne" />,
    );

    expect(container.querySelector(".ml-auto")).toBeNull();
    expect(screen.queryByRole("button")).toBeNull();
  });

  it("puts the actions at the far right", () => {
    const { container } = render(
      <ObjectHeader
        breadcrumb={[]}
        name="pve-1"
        status="online"
        stateLabel="En ligne"
        actions={<button type="button">Plan de maintenance</button>}
      />,
    );

    expect(
      screen.getByRole("button", { name: "Plan de maintenance" }),
    ).toBeInTheDocument();
    expect(container.querySelector(".ml-auto")).not.toBeNull();
  });

  // The dot repeats in colour what the tag says in words beside it, so it
  // carries no name: an image announced with nothing in it is noise in the
  // middle of a sentence.
  it("keeps the status dot out of the accessibility tree", () => {
    render(
      <ObjectHeader breadcrumb={[]} name="pve-1" status="online" stateLabel="En ligne" />,
    );

    expect(screen.queryByRole("img")).toBeNull();
  });
});
