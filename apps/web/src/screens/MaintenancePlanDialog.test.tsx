import { fireEvent, render, screen, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import type { MaintenancePlan } from "@/api/types";

import { MaintenancePlanDialog } from "./MaintenancePlanDialog";

vi.mock("@/api/useDetail", () => ({ useMaintenancePlan: vi.fn() }));

const { useMaintenancePlan } = await import("@/api/useDetail");
const planMock = vi.mocked(useMaintenancePlan);

const GIB = 1024 ** 3;

function plan(patch: Partial<MaintenancePlan> = {}): MaintenancePlan {
  return {
    cluster: "qualification",
    node: "prox-qual-2201-cit",
    fetchedAt: "2026-09-12T12:00:00Z",
    threshold: 0.8,
    feasible: true,
    moves: [
      {
        vmid: 103,
        name: "sli-airflow-sep-exp-2601-qul",
        kind: "qemu",
        status: "running",
        memory: 8 * GIB,
        target: "prox-qual-2202-cit",
        placed: true,
      },
    ],
    staying: [{ vmid: 101, name: "template-rocky10", reason: "template" }],
    targets: [
      {
        name: "prox-qual-2202-cit",
        before: { used: 20 * GIB, total: 128 * GIB, ratio: 0.15625 },
        after: { used: 28 * GIB, total: 128 * GIB, ratio: 0.21875 },
        incoming: 1,
        exceeds: false,
      },
    ],
    blockers: [],
    ...patch,
  };
}

function show(patch: Partial<MaintenancePlan> = {}, onClose = vi.fn()) {
  planMock.mockReturnValue({
    data: plan(patch),
    error: null,
    isLoading: false,
    isStale: false,
    lastUpdatedAt: new Date(),
    refresh: vi.fn(),
  });
  render(
    <MaintenancePlanDialog
      cluster="qualification"
      clusterName="Qualification"
      node="prox-qual-2201-cit"
      onClose={onClose}
    />,
  );
  return onClose;
}

afterEach(() => {
  vi.clearAllMocks();
});

describe("MaintenancePlanDialog", () => {
  it("names every guest and its destination", () => {
    show();

    // The destination appears in the migration row and again in the capacity
    // recap below, so the assertion is scoped to the row.
    const row = screen.getByText("103 · airflow-sep-exp").closest("tr");
    expect(row).not.toBeNull();
    expect(within(row as HTMLElement).getByText("prox-qual-2202-cit")).toBeInTheDocument();
    expect(within(row as HTMLElement).getByText("8 GiB")).toBeInTheDocument();
  });

  it("leaves templates where they are", () => {
    show();

    expect(screen.getByText("reste sur place")).toBeInTheDocument();
    expect(screen.getByText("template")).toBeInTheDocument();
  });

  it("states the capacity verdict before anything happens", () => {
    // This is what replaces an abstract "are you sure?".
    show();

    expect(screen.getByText(/Capacité suffisante/)).toBeInTheDocument();
    expect(screen.getByText(/prox-qual-2202-cit passe à/)).toBeInTheDocument();
  });

  it("says plainly when a guest has nowhere to go", () => {
    show({
      feasible: false,
      moves: [
        {
          vmid: 103,
          name: "big",
          kind: "qemu",
          status: "running",
          memory: 64 * GIB,
          target: "",
          placed: false,
        },
      ],
    });

    expect(screen.getByText("Aucune destination")).toBeInTheDocument();
    expect(screen.getByText(/ne trouve aucune destination/)).toBeInTheDocument();
  });

  it("translates blockers instead of leaking their keys", () => {
    show({ feasible: false, blockers: ["no_target"], moves: [], staying: [], targets: [] });

    expect(screen.getByText(/Aucun autre nœud disponible/)).toBeInTheDocument();
    expect(screen.queryByText("no_target")).not.toBeInTheDocument();
  });

  it("hands over the command rather than offering a button that cannot work", () => {
    // PVE registers node-maintenance in its CLI, not under /api2. A disabled
    // "Lancer la maintenance" would suggest the feature is merely switched off.
    show();

    expect(
      screen.getByText("ha-manager crm-command node-maintenance enable prox-qual-2201-cit"),
    ).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /Lancer/ })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /maintenance/i })).not.toBeInTheDocument();
  });

  it("closes on Escape and on the close button", () => {
    const onClose = show();

    fireEvent.keyDown(document, { key: "Escape" });
    expect(onClose).toHaveBeenCalledTimes(1);

    fireEvent.click(screen.getByRole("button", { name: "Fermer" }));
    expect(onClose).toHaveBeenCalledTimes(2);
  });

  it("is a labelled modal dialog", () => {
    show();

    const dialog = screen.getByRole("dialog");
    expect(dialog).toHaveAttribute("aria-modal", "true");
    expect(dialog).toHaveAccessibleName(/prox-qual-2201-cit/);
  });

  it("says a node with nothing on it needs no migration", () => {
    show({ moves: [], staying: [], targets: [] });

    expect(screen.getByText(/n'héberge aucune machine/)).toBeInTheDocument();
    expect(screen.getByText(/Aucune migration nécessaire/)).toBeInTheDocument();
  });
});
