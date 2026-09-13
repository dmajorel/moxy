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
        method: "online",
        ha: true,
        target: "prox-qual-2202-cit",
        placed: true,
      },
    ],
    staying: [{ vmid: 101, name: "template-rocky10", reason: "template" }],
    targets: [
      {
        name: "prox-qual-2202-cit",
        measured: true,
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
    const row = screen.getByText("sli-airflow-sep-exp-2601-qul").closest("tr");
    expect(row).not.toBeNull();
    expect(within(row as HTMLElement).getByText("prox-qual-2202-cit")).toBeInTheDocument();
    expect(within(row as HTMLElement).getByText("8 GiB")).toBeInTheDocument();
  });

  it("leaves templates where they are", () => {
    show();

    expect(screen.getByText("reste sur place")).toBeInTheDocument();
    // The one word this interface uses for that state, everywhere.
    expect(screen.getByText("Modèle")).toBeInTheDocument();
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
          method: "online",
          ha: true,
          target: "",
          placed: false,
        },
      ],
    });

    expect(screen.getByText("Aucune destination")).toBeInTheDocument();
    expect(screen.getByText(/ne trouve aucune destination/)).toBeInTheDocument();
  });

  // The CRM moves what it manages and nothing else. A guest it does not manage
  // stays on the drained node until somebody moves it, and a plan that does not
  // say so describes migrations that will not happen.
  it("tells the guests the crm will move from the ones it will not", () => {
    show({
      moves: [
        {
          vmid: 103,
          name: "airflow",
          kind: "qemu",
          status: "running",
          memory: 8 * GIB,
          method: "online",
          ha: true,
          target: "prox-qual-2202-cit",
          placed: true,
        },
        {
          vmid: 105,
          name: "runner",
          kind: "lxc",
          status: "running",
          memory: 2 * GIB,
          method: "restart",
          ha: false,
          target: "prox-qual-2202-cit",
          placed: true,
        },
      ],
    });

    expect(screen.getByText("Automatique")).toBeInTheDocument();
    expect(screen.getByText("À la main")).toBeInTheDocument();
    expect(screen.getByText(/n.est pas gérée par HA/)).toBeInTheDocument();
    // A running container cannot migrate live, so the command says --restart.
    expect(
      screen.getByText("pct migrate 105 prox-qual-2202-cit --restart"),
    ).toBeInTheDocument();
  });

  // Proxmox has no live migration for containers: a running one is stopped,
  // moved and started again. A plan that shows that beside a live VM migration
  // hides an interruption, which is the one thing this dialog exists to prevent.
  it("announces the containers that will be restarted", () => {
    show({
      moves: [
        {
          vmid: 105,
          name: "runner",
          kind: "lxc",
          status: "running",
          memory: 2 * GIB,
          method: "restart",
          ha: true,
          target: "prox-qual-2202-cit",
          placed: true,
        },
        {
          vmid: 106,
          name: "archive",
          kind: "qemu",
          status: "stopped",
          memory: 0,
          method: "offline",
          ha: true,
          target: "prox-qual-2202-cit",
          placed: true,
        },
      ],
    });

    expect(screen.getByText("Redémarrage")).toBeInTheDocument();
    expect(screen.getByText("Hors ligne")).toBeInTheDocument();
    expect(screen.getByText(/Un conteneur sera arrêté puis redémarré/)).toBeInTheDocument();
  });

  it("says when there is no ha manager to move anything", () => {
    show({
      moves: [
        {
          vmid: 103,
          name: "airflow",
          kind: "qemu",
          status: "running",
          memory: 8 * GIB,
          method: "online",
          ha: null,
          target: "prox-qual-2202-cit",
          placed: true,
        },
      ],
    });

    expect(screen.getByText(/pas de gestionnaire HA/)).toBeInTheDocument();
  });

  // Without measurements the plan looked exactly like a full cluster: every
  // guest unplaced, no blocker, and a dialog blaming the memory threshold for
  // what is a missing privilege on moxy's own token.
  it("says when the destinations have no measurements rather than blaming capacity", () => {
    show({
      feasible: false,
      blockers: ["target_stats_unavailable"],
      moves: [
        {
          vmid: 103,
          name: "airflow",
          kind: "qemu",
          status: "running",
          memory: 8 * GIB,
          method: "online",
          ha: true,
          target: "",
          placed: false,
        },
      ],
      targets: [
        {
          name: "prox-qual-2202-cit",
          measured: false,
          before: { used: 0, total: 0, ratio: 0 },
          after: { used: 0, total: 0, ratio: 0 },
          incoming: 0,
          exceeds: false,
        },
      ],
    });

    expect(screen.getByText(/Sys\.Audit sur \/nodes/)).toBeInTheDocument();
    // Not "0 % → 0 %", which would claim the node is empty and available.
    expect(screen.queryByText(/0\s*%\s*→/)).not.toBeInTheDocument();
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

  // Tab from the close button used to walk out of the dialog into the tree and
  // the top bar underneath, which is a modal that is not modal.
  it("keeps the keyboard inside", () => {
    show();

    const dialog = screen.getByRole("dialog");
    const focusable = within(dialog).getAllByRole("button");
    const first = focusable[0];
    const last = focusable[focusable.length - 1];
    if (first === undefined || last === undefined) {
      throw new Error("the dialog has no control to trap");
    }

    // The first control takes the focus on open: the close button is the way
    // out, so Escape and Enter both do something from the first keystroke.
    expect(document.activeElement).toBe(first);

    last.focus();
    fireEvent.keyDown(document, { key: "Tab" });
    expect(document.activeElement).toBe(first);

    fireEvent.keyDown(document, { key: "Tab", shiftKey: true });
    expect(document.activeElement).toBe(last);
  });

  // Closing dropped the focus on <body>, returning a keyboard user to the top
  // of the document rather than to the button they had pressed. CLAUDE.md
  // already requires it of every menu: "se ferment à Échap en rendant le
  // focus".
  it("hands the focus back to whatever opened it", () => {
    const trigger = document.createElement("button");
    trigger.textContent = "Plan de maintenance";
    document.body.append(trigger);
    trigger.focus();
    expect(document.activeElement).toBe(trigger);

    planMock.mockReturnValue({
      data: plan(),
      error: null,
      isLoading: false,
      isStale: false,
      lastUpdatedAt: new Date(),
      refresh: vi.fn(),
    });
    const view = render(
      <MaintenancePlanDialog
        cluster="qualification"
        clusterName="Qualification"
        node="prox-qual-2201-cit"
        onClose={vi.fn()}
      />,
    );
    expect(document.activeElement).not.toBe(trigger);

    view.unmount();
    expect(document.activeElement).toBe(trigger);
    trigger.remove();
  });

  // The only Tailwind palette colour left in the repository, and black at 45 %
  // over #101216 was very nearly invisible: the dialog floated with nothing
  // behind it in the dark theme.
  it("veils the page with a token, not with a palette colour", () => {
    show();

    const scrim = screen.getByRole("dialog").parentElement;
    expect(scrim).not.toBeNull();
    expect(scrim?.className).toContain("bg-scrim");
    expect(scrim?.className).not.toContain("bg-black");
  });
});
