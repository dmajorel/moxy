import { render, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import type { GuestDetail as GuestDetailData, Task } from "@/api/types";

import { GuestDetail } from "./GuestDetail";

const GIB = 1024 ** 3;

function guest(patch: Partial<GuestDetailData> = {}): GuestDetailData {
  return {
    cluster: "qualification",
    node: "prox-qual-2201-cit",
    vmid: 103,
    name: "sli-airflow-sep-exp-2601-qul",
    kind: "qemu",
    status: "running",
    uptime: 252_000,
    fetchedAt: "2026-09-12T12:00:00Z",
    cpu: { ratio: 0.0025, cores: 6 },
    memory: { used: 1.25 * GIB, total: 8 * GIB, ratio: 0.15625 },
    disk: { used: 0, total: 28 * GIB, ratio: 0 },
    hostMemory: 1.57 * GIB,
    tags: ["env.qualification", "backup.none"],
    haState: "started",
    ipv4: "10.18.160.4",
    ...patch,
  };
}

function task(patch: Partial<Task> = {}): Task {
  return {
    upid: `UPID:${Math.random().toString(36).slice(2)}`,
    node: "prox-qual-2201-cit",
    type: "vzdump",
    id: "103",
    user: "root@pam",
    start: new Date(2026, 8, 12, 4, 26, 34).toISOString(),
    end: new Date(2026, 8, 12, 4, 26, 38).toISOString(),
    duration: 4,
    status: "OK",
    ok: true,
    ...patch,
  };
}

function renderGuest(patch: Partial<GuestDetailData> = {}, tasks: Task[] = []) {
  return render(
    <GuestDetail
      guest={guest(patch)}
      clusterName="Qualification"
      series={null}
      tasks={tasks}
      threshold={0.8}
    />,
  );
}

describe("GuestDetail", () => {
  it("puts the state and the tags beside the name", () => {
    renderGuest();

    const header = screen
      .getByRole("heading", { name: "sli-airflow-sep-exp-2601-qul" })
      .closest("header");
    expect(header).not.toBeNull();
    expect(within(header as HTMLElement).getByText(/En cours/)).toBeInTheDocument();
    expect(within(header as HTMLElement).getByText("env.qualification")).toBeInTheDocument();
  });

  it("shows an allocated boot disk rather than a fake zero usage", () => {
    // Proxmox only knows what a guest consumes when its agent reports it, so
    // used = 0 means "not reported", not "empty".
    renderGuest();

    const card = screen.getByText("Disque de boot").closest("div");
    expect(card).not.toBeNull();
    expect(within(card as HTMLElement).getByText("· alloué")).toBeInTheDocument();
    expect(within(card as HTMLElement).queryByRole("progressbar")).not.toBeInTheDocument();
  });

  it("shows real usage when the agent does report it", () => {
    renderGuest({ disk: { used: 12 * GIB, total: 28 * GIB, ratio: 12 / 28 } });

    const card = screen.getByText("Disque de boot").closest("div");
    expect(within(card as HTMLElement).getByRole("progressbar")).toBeInTheDocument();
  });

  it("renders an em dash for a missing agent address", () => {
    renderGuest({ ipv4: null, haState: null });

    const ip = screen.getByText("IPv4").closest("div");
    expect(within(ip as HTMLElement).getByText("—")).toBeInTheDocument();
  });

  it("drops the uptime for a stopped guest instead of showing zero", () => {
    renderGuest({ status: "stopped", uptime: 0 });

    const header = screen.getByRole("heading", { level: 1 }).closest("header");
    expect(within(header as HTMLElement).getByText("Arrêtée")).toBeInTheDocument();
    expect(within(header as HTMLElement).queryByText(/·\s*0/)).not.toBeInTheDocument();
  });

  it("labels a container as such", () => {
    renderGuest({ kind: "lxc" });
    expect(screen.getByText("Conteneur LXC")).toBeInTheDocument();
  });

  it("shows the task duration rather than two timestamps to subtract", () => {
    renderGuest({}, [task()]);

    expect(screen.getByText("Sauvegarde · 103")).toBeInTheDocument();
    expect(screen.getByText("4 s")).toBeInTheDocument();
    expect(screen.getByText("OK")).toBeInTheDocument();
  });

  it("marks a running task without inventing a duration", () => {
    renderGuest({}, [task({ end: null, duration: null, status: "running", ok: null })]);

    expect(screen.getByText("En cours")).toBeInTheDocument();
    const row = screen.getByText("Sauvegarde · 103").closest("tr");
    expect(within(row as HTMLElement).getByText("—")).toBeInTheDocument();
  });

  it("keeps a failed task's raw error out of the row", () => {
    // The PVE error string is diagnostic material, not a label.
    renderGuest({}, [
      task({ ok: false, status: "storage 'nfs' is not online" }),
    ]);

    expect(screen.getByText("Échec")).toBeInTheDocument();
    expect(screen.queryByText(/is not online/)).not.toBeInTheDocument();
    expect(screen.getByTitle("storage 'nfs' is not online")).toBeInTheDocument();
  });

  it("says so when the journal holds nothing for this machine", () => {
    renderGuest({}, []);
    expect(screen.getByText(/Aucune tâche récente pour cette machine/)).toBeInTheDocument();
  });
});
