import { render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import type { Task, Tasks } from "@/api/types";

import { ClusterJournal } from "./ClusterJournal";

vi.mock("@/api/useDetail", () => ({ useTasks: vi.fn() }));

const { useTasks } = await import("@/api/useDetail");
const tasksMock = vi.mocked(useTasks);

function task(patch: Partial<Task> = {}): Task {
  return {
    upid: "UPID:pve-01:1",
    node: "pve-01",
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

function state(data: Tasks | null, patch: Partial<ReturnType<typeof useTasks>> = {}) {
  tasksMock.mockReturnValue({
    data,
    error: null,
    isLoading: false,
    isStale: false,
    lastUpdatedAt: new Date(),
    refresh: vi.fn(),
    ...patch,
  });
}

afterEach(() => {
  vi.clearAllMocks();
});

describe("ClusterJournal", () => {
  it("lists the tasks with their computed duration", () => {
    state({ cluster: "qual", fetchedAt: "", entries: [task()] });

    render(<ClusterJournal cluster="qual" />);

    expect(screen.getByText("Journal du cluster")).toBeInTheDocument();
    expect(screen.getByText("Sauvegarde · 103")).toBeInTheDocument();
    expect(screen.getByText("4 s")).toBeInTheDocument();
  });

  it("says the connection was lost without hiding the entries", () => {
    state({ cluster: "qual", fetchedAt: "", entries: [task()] }, { isStale: true });

    render(<ClusterJournal cluster="qual" />);

    expect(screen.getByText("Connexion perdue")).toBeInTheDocument();
    expect(screen.getByText("Sauvegarde · 103")).toBeInTheDocument();
  });

  it("degrades to a message rather than an empty frame", () => {
    state(null, { error: new Error("upstream unavailable") });

    render(<ClusterJournal cluster="qual" />);

    expect(screen.getByText(/Journal indisponible/)).toBeInTheDocument();
    // The English backend message is diagnostic material, never a label.
    expect(screen.queryByText(/upstream/)).not.toBeInTheDocument();
  });

  it("says so when the cluster has no recent task", () => {
    state({ cluster: "qual", fetchedAt: "", entries: [] });

    render(<ClusterJournal cluster="qual" />);

    expect(screen.getByText("Aucune tâche récente.")).toBeInTheDocument();
  });
});
