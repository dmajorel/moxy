import { render, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import type { Task } from "@/api/types";

import { TasksTable } from "./TasksTable";

function task(patch: Partial<Task> = {}): Task {
  return {
    upid: `UPID:${Math.random().toString(36).slice(2)}`,
    node: "prox-pprd-2301-cit",
    type: "vzdump",
    id: "103",
    user: "root@pam",
    start: new Date(2026, 8, 12, 4, 26, 34).toISOString(),
    end: new Date(2026, 8, 12, 4, 26, 38).toISOString(),
    duration: 4,
    status: "OK",
    outcome: "ok",
    warnings: null,
    ...patch,
  };
}

function rowOf(label: string): HTMLElement {
  const row = screen.getByText(label).closest("tr");
  if (row === null) throw new Error(`no row for ${label}`);
  return row;
}

describe("TasksTable", () => {
  it("says so when there is nothing to show", () => {
    render(<TasksTable entries={[]} emptyHint="Aucune tâche récente." />);
    expect(screen.getByText("Aucune tâche récente.")).toBeInTheDocument();
  });

  it("shows the duration the backend computed, not two timestamps", () => {
    render(<TasksTable entries={[task()]} />);

    const row = rowOf("Sauvegarde · 103");
    expect(within(row).getByText("4 s")).toBeInTheDocument();
    expect(within(row).getByText("OK")).toBeInTheDocument();
  });

  it("leaves a running task without a duration", () => {
    render(
      <TasksTable
        entries={[task({ end: null, duration: null, status: "running", outcome: "running" })]}
      />,
    );

    const row = rowOf("Sauvegarde · 103");
    expect(within(row).getByText("En cours")).toBeInTheDocument();
    expect(within(row).getByText("—")).toBeInTheDocument();
  });

  // The bug this table had: PVE ends a job that warned with "WARNINGS: 2", and
  // "anything but OK is a failure" turned every nightly vzdump that warned
  // about one guest into a red line -- a false alarm every night, on the one
  // task operators watch hardest.
  it("renders a task that warned as warnings, never as a failure", () => {
    render(
      <TasksTable
        entries={[task({ status: "WARNINGS: 2", outcome: "warnings", warnings: 2 })]}
      />,
    );

    const row = rowOf("Sauvegarde · 103");
    expect(within(row).getByText("Avertissements (2)")).toBeInTheDocument();
    expect(within(row).queryByText("Échec")).toBeNull();
    // The raw string stays reachable for whoever needs to know more.
    expect(within(row).getByTitle("WARNINGS: 2")).toBeInTheDocument();
  });

  it("keeps the count out of the label when there is none", () => {
    render(
      <TasksTable
        entries={[task({ status: "WARNINGS: many", outcome: "warnings", warnings: null })]}
      />,
    );

    expect(screen.getByText("Avertissements")).toBeInTheDocument();
  });

  it("keeps a failure's raw error out of the row", () => {
    render(
      <TasksTable
        entries={[
          task({ status: "storage 'nfs-shared' is not online", outcome: "failed" }),
        ]}
      />,
    );

    const row = rowOf("Sauvegarde · 103");
    expect(within(row).getByText("Échec")).toBeInTheDocument();
    expect(within(row).queryByText(/is not online/)).toBeNull();
    expect(within(row).getByTitle("storage 'nfs-shared' is not online")).toBeInTheDocument();
  });

  // Colour alone never carries the verdict -- the four outcomes read as four
  // different words, which is what a screen reader and a colour-blind operator
  // get.
  it("gives each outcome its own wording", () => {
    render(
      <TasksTable
        entries={[
          task({ upid: "a", id: "1", outcome: "ok" }),
          task({ upid: "b", id: "2", outcome: "warnings", warnings: 3, status: "WARNINGS: 3" }),
          task({ upid: "c", id: "3", outcome: "failed", status: "boom" }),
          task({ upid: "d", id: "4", outcome: "running", end: null, duration: null }),
        ]}
      />,
    );

    const labels = ["OK", "Avertissements (3)", "Échec", "En cours"];
    for (const label of labels) {
      expect(screen.getByText(label)).toBeInTheDocument();
    }
    expect(new Set(labels).size).toBe(labels.length);
  });

  // Without scope, a screen reader has to guess which cells a header governs,
  // and a table with no caption is announced as "table" and nothing else.
  it("declares its column headers and names itself", () => {
    render(<TasksTable entries={[task()]} />);

    const table = screen.getByRole("table");
    expect(table).toHaveAccessibleName(/Tâches récentes/);
    for (const header of within(table).getAllByRole("columnheader")) {
      expect(header).toHaveAttribute("scope", "col");
    }
  });
});
