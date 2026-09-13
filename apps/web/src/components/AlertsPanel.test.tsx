import { fireEvent, render, screen, within } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { AlertsPanel } from "./AlertsPanel";
import type { AlertEntry } from "./AlertsPanel";

function entry(patch: Partial<AlertEntry> = {}): AlertEntry {
  return {
    clusterId: "pprd",
    clusterName: "Préproduction",
    status: "degraded",
    alert: { kind: "memory_high", ratio: 0.92, nodes: ["prox-pprd-2301-cit"] },
    ...patch,
  };
}

function open(alerts: AlertEntry[] = [entry()]) {
  const onSelectCluster = vi.fn();
  render(<AlertsPanel alerts={alerts} onSelectCluster={onSelectCluster} />);
  const bell = screen.getByRole("button", { name: /Notifications/ });
  fireEvent.click(bell);
  return { onSelectCluster, bell };
}

describe("AlertsPanel", () => {
  it("counts the alerts on the bell, and agrees in number", () => {
    render(<AlertsPanel alerts={[entry()]} onSelectCluster={vi.fn()} />);
    expect(
      screen.getByRole("button", { name: "Notifications · 1 alerte" }),
    ).toBeInTheDocument();
  });

  it("draws no counter on a healthy estate", () => {
    render(<AlertsPanel alerts={[]} onSelectCluster={vi.fn()} />);

    const bell = screen.getByRole("button", { name: "Notifications" });
    expect(bell).toHaveTextContent("");
  });

  // The cards show alerts[0] only and the header counts them all: this panel
  // is the one place the rest of them exist.
  it("lists every alert, with the cluster it belongs to", () => {
    open([
      entry(),
      entry({
        clusterId: "prod",
        clusterName: "Production",
        status: "healthy",
        alert: { kind: "updates_available", version: "9.2.12", nodes: ["a", "b"] },
      }),
    ]);

    const menu = screen.getByRole("menu", { name: "Alertes" });
    const items = within(menu).getAllByRole("menuitem");
    expect(items).toHaveLength(2);
    expect(items[0]).toHaveTextContent("Préproduction");
    expect(items[1]).toHaveTextContent("Production");
    expect(items[1]).toHaveTextContent("Mise à jour 9.2.12 disponible sur 2 nœuds");
  });

  it("opens the cluster an alert belongs to, and closes", () => {
    const { onSelectCluster } = open();

    fireEvent.click(screen.getByRole("menuitem"));

    expect(onSelectCluster).toHaveBeenCalledWith("pprd");
    expect(screen.queryByRole("menu")).toBeNull();
  });

  it("says so rather than showing an empty list", () => {
    open([]);

    expect(screen.getByText("Aucune alerte")).toBeInTheDocument();
    expect(screen.queryByRole("menuitem")).toBeNull();
  });

  // Every menu of this bar closes on Escape and hands the focus back: a
  // popover that keeps it is a trap.
  it("closes on Escape and returns the focus to the bell", () => {
    const { bell } = open();

    fireEvent.keyDown(bell, { key: "Escape" });

    expect(screen.queryByRole("menu")).toBeNull();
    expect(document.activeElement).toBe(bell);
  });

  it("closes on a second click of the bell", () => {
    const { bell } = open();

    fireEvent.click(bell);

    expect(screen.queryByRole("menu")).toBeNull();
  });

  it("opens from the keyboard and lands on the first entry", () => {
    render(<AlertsPanel alerts={[entry(), entry()]} onSelectCluster={vi.fn()} />);
    const bell = screen.getByRole("button", { name: /Notifications/ });

    fireEvent.keyDown(bell, { key: "ArrowDown" });

    const items = screen.getAllByRole("menuitem");
    expect(document.activeElement).toBe(items[0]);
  });

  it("wraps the arrows around the list", () => {
    open([entry({ clusterId: "a" }), entry({ clusterId: "b" })]);
    const items = screen.getAllByRole("menuitem");
    const menu = screen.getByRole("menu");

    fireEvent.keyDown(menu, { key: "ArrowUp" });
    expect(document.activeElement).toBe(items[items.length - 1]);

    fireEvent.keyDown(menu, { key: "ArrowDown" });
    expect(document.activeElement).toBe(items[0]);
  });

  // A menu that outlives a click elsewhere is a trap; the focus only goes back
  // to the bell when it was still inside.
  it("closes when the click lands outside it", () => {
    open();

    fireEvent.mouseDown(document.body);

    expect(screen.queryByRole("menu")).toBeNull();
  });

  it("says how many alerts it holds, in the plural", () => {
    render(
      <AlertsPanel alerts={[entry(), entry(), entry()]} onSelectCluster={vi.fn()} />,
    );

    expect(
      screen.getByRole("button", { name: "Notifications · 3 alertes" }),
    ).toHaveTextContent("3");
  });
});
