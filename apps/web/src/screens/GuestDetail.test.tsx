import {
  fireEvent,
  getDefaultNormalizer,
  render,
  screen,
  within,
} from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import type {
  GuestDetail as GuestDetailData,
  Task,
  Thresholds,
} from "@/api/types";
import { NNBSP } from "@/lib/format";

import { GuestDetail } from "./GuestDetail";

/** The narrow no-break space before a `%` survives only without collapsing. */
const EXACT = { normalizer: getDefaultNormalizer({ collapseWhitespace: false }) };

/**
 * One figure for the three resources, which is what the defaults are: a test
 * that needs them apart says so on the spot.
 */
function evenly(ratio: number): Thresholds {
  return { memory: ratio, cpu: ratio, storage: ratio };
}

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
    // Unmeasured, which is the common case: only a guest agent reports what a
    // guest consumes, and the payload says so with a null rather than a zero.
    disk: { used: null, total: 28 * GIB, ratio: null },
    disks: [
      {
        key: "scsi0",
        storage: "ceph-vm",
        volume: "ceph-vm:vm-103-disk-0",
        size: 28 * GIB,
        attached: true,
      },
      {
        key: "scsi1",
        storage: "ceph-vm",
        volume: "ceph-vm:vm-103-disk-1",
        size: 2048 * GIB,
        attached: true,
      },
    ],
    allocated: { bytes: 2076 * GIB, partial: false, detached: 0, detachedBytes: 0 },
    nets: [
      // A named network, and a bridge nobody named: the two cases the block
      // has to render differently.
      { key: "net0", name: null, bridge: "vmbr1", alias: "DMZ publique", tag: 120, mac: "BC:24:11:AA:BB:CC" },
      { key: "net1", name: null, bridge: "vmbr0", alias: null, tag: null, mac: "BC:24:11:AA:BB:DD" },
    ],
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
    outcome: "ok",
    warnings: null,
    ...patch,
  };
}

function renderGuest(patch: Partial<GuestDetailData> = {}, tasks: Task[] = []) {
  return render(
    <GuestDetail
      guest={guest(patch)}
      clusterName="Qualification"
      series={null}
      timeframe="hour"
      tasks={tasks}
      thresholds={evenly(0.8)}
    />,
  );
}

describe("GuestDetail", () => {
  // Same rule as the node view: the boot disk is not coloured by the memory
  // limit.
  it("colours each metric card by the threshold of its own resource", () => {
    render(
      <GuestDetail
        guest={guest({
          cpu: { ratio: 0.75, cores: 6 },
          memory: { used: 6.8 * GIB, total: 8 * GIB, ratio: 0.85 },
          disk: { used: 24 * GIB, total: 32 * GIB, ratio: 0.75 },
          allocated: null,
        })}
        clusterName="Qualification"
        series={null}
        timeframe="hour"
        tasks={[]}
        thresholds={{ memory: 0.9, cpu: 0.8, storage: 0.7 }}
      />,
    );

    const fillOf = (label: string) =>
      screen.getByRole("progressbar", { name: label }).firstElementChild;

    expect(fillOf("CPU")).toHaveClass("bg-accent");
    expect(fillOf("Mémoire")).toHaveClass("bg-accent");
    expect(fillOf("Disque de boot")).toHaveClass("bg-warning");
  });

  it("keeps the name line for the state, not for the tags", () => {
    renderGuest();

    const header = screen
      .getByRole("heading", { name: "sli-airflow-sep-exp-2601-qul" })
      .closest("header");
    expect(header).not.toBeNull();
    expect(within(header as HTMLElement).getByText(/En cours/)).toBeInTheDocument();
    expect(within(header as HTMLElement).getByText("Machine virtuelle")).toBeInTheDocument();
    // Ten tags on a real guest would push the state out of the line.
    expect(within(header as HTMLElement).queryByText("env.qualification")).toBeNull();
    expect(within(header as HTMLElement).queryByText(/qualification/)).toBeNull();
  });

  it("lists the tags as key/value pairs, cut at the last dot", () => {
    renderGuest({ tags: ["ha.state.started", "env.qualification", "production"] });

    const tags = screen.getByText("Étiquettes").closest("section");
    expect(tags).not.toBeNull();
    const rows = within(tags as HTMLElement);

    // The key is the namespace, however deep: "ha.state", never "ha".
    expect(rows.getByText("ha.state")).toBeInTheDocument();
    expect(rows.getByText("started")).toBeInTheDocument();
    expect(rows.getByText("env")).toBeInTheDocument();
    expect(rows.getByText("qualification")).toBeInTheDocument();
    // A flag tag names without qualifying: no value to show.
    expect(rows.getByText("production")).toBeInTheDocument();
    expect(rows.getByText("—")).toBeInTheDocument();
  });

  it("keeps the order PVE serves rather than sorting the keys", () => {
    renderGuest({ tags: ["zone.dmz", "env.qualification", "backup.none"] });

    const tags = screen.getByText("Étiquettes").closest("section");
    const keys = within(tags as HTMLElement)
      .getAllByRole("term")
      .map((term) => term.textContent);
    expect(keys).toEqual(["zone", "env", "backup"]);
  });

  it("renders two tags of the same key as two rows", () => {
    // A migrated guest carries both. Keying the rows by label would warn and
    // render unstably, so the panel keys them by position.
    const warn = vi.spyOn(console, "error").mockImplementation(() => undefined);
    renderGuest({ tags: ["env.prod", "env.test"] });

    const tags = screen.getByText("Étiquettes").closest("section");
    expect(within(tags as HTMLElement).getAllByText("env")).toHaveLength(2);
    expect(warn).not.toHaveBeenCalled();
    warn.mockRestore();
  });

  it("shows no tag section at all for a guest that carries none", () => {
    renderGuest({ tags: [] });
    expect(screen.queryByText("Étiquettes")).toBeNull();
  });

  it("totals every volume rather than showing the boot disk alone", () => {
    // The whole point of the card: maxdisk is the boot disk, and a guest with
    // a data disk allocates far more than it. No bar either — what a guest
    // consumes across its volumes is unknown unless an agent says so.
    renderGuest();

    const card = screen.getByText("Volumétrie").closest("div");
    expect(card).not.toBeNull();
    expect(within(card as HTMLElement).getByText("2 TiB")).toBeInTheDocument();
    expect(within(card as HTMLElement).getByText("· alloué")).toBeInTheDocument();
    expect(within(card as HTMLElement).queryByRole("progressbar")).not.toBeInTheDocument();
  });

  it("calls the total a floor when a volume declares no size", () => {
    renderGuest({
      disks: [
        { key: "scsi0", storage: "ceph-vm", volume: "ceph-vm:vm-103-disk-0", size: 28 * GIB, attached: true },
        { key: "scsi1", storage: null, volume: "/dev/sdb", size: null, attached: true },
      ],
      allocated: { bytes: 28 * GIB, partial: true, detached: 0, detachedBytes: 0 },
    });

    const card = screen.getByText("Volumétrie").closest("div");
    expect(within(card as HTMLElement).getByText("· au moins")).toBeInTheDocument();
  });

  it("lists the volumes, dashing the size nobody knows", () => {
    renderGuest({
      disks: [
        { key: "scsi0", storage: "ceph-vm", volume: "ceph-vm:vm-103-disk-0", size: 28 * GIB, attached: true },
        { key: "scsi1", storage: null, volume: "/dev/sdb", size: null, attached: true },
      ],
      allocated: { bytes: 28 * GIB, partial: true, detached: 0, detachedBytes: 0 },
    });

    expect(screen.getByText("2 disques")).toBeInTheDocument();
    const row = screen.getByText("/dev/sdb").closest("tr");
    expect(row).not.toBeNull();
    // A passed-through device belongs to no storage and declares no size:
    // both are unknown, and an unknown is a dash, never a zero.
    expect(within(row as HTMLElement).getAllByText("—")).toHaveLength(2);
  });

  // The storage has a column of its own, and the volume id begins with it:
  // printing both spelled it twice on every line.
  it("names the storage once, not again in front of the volume", () => {
    renderGuest({
      disks: [
        { key: "scsi0", storage: "ceph-vm", volume: "ceph-vm:vm-103-disk-0", size: 28 * GIB, attached: true },
      ],
      allocated: { bytes: 28 * GIB, partial: false, detached: 0, detachedBytes: 0 },
    });

    const row = screen.getByText("vm-103-disk-0").closest("tr");
    expect(row).not.toBeNull();
    const scope = within(row as HTMLElement);
    expect(scope.getByText("ceph-vm")).toBeInTheDocument();
    expect(scope.queryByText("ceph-vm:vm-103-disk-0")).toBeNull();
  });

  it("marks a detached volume and keeps it out of the total", () => {
    renderGuest({
      disks: [
        { key: "scsi0", storage: "ceph-vm", volume: "ceph-vm:vm-103-disk-0", size: 28 * GIB, attached: true },
        { key: "unused0", storage: "local-lvm", volume: "local-lvm:vm-103-disk-3", size: null, attached: false },
      ],
      allocated: { bytes: 28 * GIB, partial: false, detached: 1, detachedBytes: 0 },
    });

    const row = screen.getByText("vm-103-disk-3").closest("tr");
    expect(within(row as HTMLElement).getByText("Détaché")).toBeInTheDocument();
    expect(screen.getByText(/1 volume détaché · hors total/)).toBeInTheDocument();
  });

  it("falls back to the boot disk when the configuration could not be read", () => {
    // No VM.Audit on the guest: PVE answers 403, the list stays null and the
    // page is served all the same, with the one figure the status endpoint
    // does carry.
    renderGuest({
      disks: null,
      allocated: null,
      disk: { used: 12 * GIB, total: 28 * GIB, ratio: 12 / 28 },
    });

    expect(screen.queryByText("Volumétrie")).not.toBeInTheDocument();
    expect(screen.queryByText("Disques")).not.toBeInTheDocument();
    const card = screen.getByText("Disque de boot").closest("div");
    expect(within(card as HTMLElement).getByRole("progressbar")).toBeInTheDocument();
  });

  it("reports the boot disk usage the agent does give", () => {
    renderGuest({ disk: { used: 12 * GIB, total: 28 * GIB, ratio: 12 / 28 } });

    const row = screen.getByText("Disque de boot").closest("div");
    expect(
      within(row as HTMLElement).getByText(`43${NNBSP}% · 12 / 28 GiB`, EXACT),
    ).toBeInTheDocument();
  });

  // Without an agent the used half is null, not zero: a volume carrying a
  // filesystem is never genuinely empty, so "0 / 28 GiB" was a claim nobody
  // could make. The card shows the declared size, says it is an allocation,
  // and draws no fill level.
  it("shows the declared size alone when nothing measured the boot disk", () => {
    renderGuest({
      disks: null,
      allocated: null,
      disk: { used: null, total: 28 * GIB, ratio: null },
    });

    const card = screen.getByText("Disque de boot").closest("div");
    expect(within(card as HTMLElement).getByText("28 GiB")).toBeInTheDocument();
    expect(within(card as HTMLElement).getByText("· alloué")).toBeInTheDocument();
    expect(within(card as HTMLElement).queryByRole("progressbar")).toBeNull();
    expect(within(card as HTMLElement).queryByText(/0 \//)).toBeNull();
  });

  // With the configuration readable the volumetry card takes the metric slot,
  // and the boot disk moves to the key/value list -- where an unmeasured one
  // is the em dash, as every other unknown of this API is.
  it("renders the em dash for an unmeasured boot disk in the summary list", () => {
    renderGuest({ disk: { used: null, total: 28 * GIB, ratio: null } });

    const row = screen.getByText("Disque de boot").closest("div");
    expect(within(row as HTMLElement).getByText("—")).toBeInTheDocument();
  });

  it("renders an em dash for a missing agent address", () => {
    renderGuest({ ipv4: null, haState: null });

    const ip = screen.getByText("IPv4").closest("div");
    expect(within(ip as HTMLElement).getByText("—")).toBeInTheDocument();
  });

  it("drops the uptime for a stopped guest instead of showing zero", () => {
    renderGuest({ status: "stopped", uptime: null });

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
    renderGuest({}, [task({ end: null, duration: null, status: "running", outcome: "running" })]);

    expect(screen.getByText("En cours")).toBeInTheDocument();
    const row = screen.getByText("Sauvegarde · 103").closest("tr");
    expect(within(row as HTMLElement).getByText("—")).toBeInTheDocument();
  });

  it("keeps a failed task's raw error out of the row", () => {
    // The PVE error string is diagnostic material, not a label.
    renderGuest({}, [
      task({ outcome: "failed", status: "storage 'nfs' is not online" }),
    ]);

    expect(screen.getByText("Échec")).toBeInTheDocument();
    expect(screen.queryByText(/is not online/)).not.toBeInTheDocument();
    expect(screen.getByTitle("storage 'nfs' is not online")).toBeInTheDocument();
  });

  it("says so when the journal holds nothing for this machine", () => {
    renderGuest({}, []);
    expect(screen.getByText(/Aucune tâche récente pour cette machine/)).toBeInTheDocument();
  });

  // A drift over the week does not show in the hour just gone, and the fixed
  // [0, 1] scale is what makes the two windows comparable at all.
  it("asks for another window when one is picked", () => {
    const onTimeframeChange = vi.fn();
    render(
      <GuestDetail
        guest={guest()}
        clusterName="Qualification"
        series={null}
        timeframe="hour"
        onTimeframeChange={onTimeframeChange}
        tasks={[]}
        thresholds={evenly(0.8)}
      />,
    );

    fireEvent.click(screen.getByRole("radio", { name: "7 derniers jours" }));

    expect(onTimeframeChange).toHaveBeenCalledWith("week");
  });

  it("draws no picker without a handler", () => {
    renderGuest();

    expect(screen.queryByRole("radiogroup")).not.toBeInTheDocument();
  });

  // The caption names the window the payload carries, not the button that is
  // lit: a reading in flight when the window changed keeps its own span.
  it("captions the window the series says it holds", () => {
    render(
      <GuestDetail
        guest={guest()}
        clusterName="Qualification"
        series={{
          cluster: "qualification",
          timeframe: "day",
          fetchedAt: "2026-09-12T12:00:00Z",
          points: [
            { time: new Date(2026, 8, 11, 12, 0).toISOString(), cpu: 0.1, memUsed: null, memTotal: null, netIn: null, netOut: null },
            { time: new Date(2026, 8, 12, 0, 0).toISOString(), cpu: 0.2, memUsed: null, memTotal: null, netIn: null, netOut: null },
            { time: new Date(2026, 8, 12, 12, 0).toISOString(), cpu: 0.3, memUsed: null, memTotal: null, netIn: null, netOut: null },
          ],
          cpuAverage: 0.2,
        }}
        timeframe="day"
        tasks={[]}
        thresholds={evenly(0.8)}
      />,
    );

    expect(screen.getByText(/^Dernières 24 h · moy\./)).toBeInTheDocument();
  });
});

describe("GuestDetail networks", () => {
  // The reason the block exists: an operator checking a machine sits on the
  // right network reads the name someone gave it, not vmbr12.
  it("leads with the network name and keeps the bridge beside it", () => {
    renderGuest();

    const block = screen.getByText("Réseaux").closest("section");
    expect(block).not.toBeNull();
    const scope = within(block as HTMLElement);
    expect(scope.getByText("DMZ publique")).toBeInTheDocument();
    expect(scope.getByText("vmbr1")).toBeInTheDocument();
    expect(scope.getByText("VLAN 120")).toBeInTheDocument();
  });

  // A bridge nobody named is NOT unknown. Falling back to the em dash here
  // would replace a usable answer with nothing.
  it("shows the bridge itself when the network carries no alias", () => {
    renderGuest({
      // A MAC is supplied so that the only cell that could dash is the
      // network one: an unknown MAC dashes on purpose, and would otherwise
      // be mistaken for the fallback under test.
      nets: [
        { key: "net0", name: null, bridge: "vmbr0", alias: null, tag: null, mac: "BC:24:11:AA:BB:CC" },
      ],
    });

    const scope = within(screen.getByText("Réseaux").closest("section") as HTMLElement);
    expect(scope.getByText("vmbr0")).toBeInTheDocument();
    expect(scope.queryByText("—")).toBeNull();
  });

  // A container names the interface its own system sees; a VM never does.
  it("adds the guest-side name when the configuration carries one", () => {
    renderGuest({
      kind: "lxc",
      nets: [{ key: "net0", name: "eth0", bridge: "vmbr0", alias: null, tag: null, mac: null }],
    });

    const scope = within(screen.getByText("Réseaux").closest("section") as HTMLElement);
    expect(scope.getByText("eth0")).toBeInTheDocument();
  });

  // The block sits beside the volumes rather than under them, which is what
  // the two-column row buys.
  it("puts the networks on the same row as the volumes", () => {
    renderGuest();

    const disks = screen.getByText("Disques").closest("section");
    const nets = screen.getByText("Réseaux").closest("section");
    expect(disks?.parentElement).toBe(nets?.parentElement);
    expect(disks?.parentElement?.className).toContain("grid");
  });

  // One 403 on the configuration takes both lists away at once; neither block
  // may then claim the guest has no card.
  it("drops the block entirely when the configuration could not be read", () => {
    renderGuest({ nets: null, disks: null, allocated: null });

    expect(screen.queryByText("Réseaux")).toBeNull();
    expect(screen.queryByText("Disques")).toBeNull();
  });

  it("says so when the guest genuinely declares no interface", () => {
    renderGuest({ nets: [] });

    const scope = within(screen.getByText("Réseaux").closest("section") as HTMLElement);
    expect(scope.getByText("Ce système ne déclare aucune interface.")).toBeInTheDocument();
  });
});
