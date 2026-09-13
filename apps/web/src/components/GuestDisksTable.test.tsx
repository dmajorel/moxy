import { render, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import type { GuestDisk } from "@/api/types";
import { FALLBACK } from "@/lib/format";

import { GuestDisksTable } from "./GuestDisksTable";

const GIB = 1024 ** 3;
const TIB = 1024 ** 4;

function disk(patch: Partial<GuestDisk> = {}): GuestDisk {
  return {
    key: "scsi0",
    storage: "ceph-vm",
    volume: "ceph-vm:vm-103-disk-0",
    size: 32 * GIB,
    attached: true,
    ...patch,
  };
}

function rowOf(key: string): HTMLElement {
  const row = screen.getByText(key).closest("tr");
  if (row === null) throw new Error(`no row for ${key}`);
  return row;
}

describe("GuestDisksTable", () => {
  // The reason the table exists: a VM with a 32 GiB system disk and a 2 TiB
  // data disk read as "32 GiB", and there was nowhere to see otherwise
  // without leaving moxy.
  it("lists every volume with its storage and its size", () => {
    render(
      <GuestDisksTable
        disks={[
          disk(),
          disk({ key: "scsi1", volume: "ceph-vm:vm-103-disk-1", size: 2 * TIB }),
        ]}
      />,
    );

    expect(within(rowOf("scsi0")).getByText("ceph-vm")).toBeInTheDocument();
    expect(within(rowOf("scsi0")).getByText("32 GiB")).toBeInTheDocument();
    expect(within(rowOf("scsi1")).getByText("2 TiB")).toBeInTheDocument();
  });

  // A device passed straight through declares no size, and neither does a
  // detached volume. A zero would claim the volume takes no room.
  it("renders the em dash for a size nobody records", () => {
    render(<GuestDisksTable disks={[disk({ key: "hostpci0", size: null })]} />);

    const row = rowOf("hostpci0");
    expect(within(row).getByText(FALLBACK)).toBeInTheDocument();
    expect(within(row).queryByText("0 o")).toBeNull();
  });

  // A detached volume still occupies its storage, so it is listed — but it is
  // not part of what the running guest uses, so it is marked.
  it("marks a detached volume without hiding it", () => {
    render(
      <GuestDisksTable
        disks={[disk(), disk({ key: "unused0", attached: false, size: 8 * GIB })]}
      />,
    );

    expect(within(rowOf("unused0")).getByText("Détaché")).toBeInTheDocument();
    expect(within(rowOf("scsi0")).queryByText("Détaché")).toBeNull();
  });

  it("renders the em dash for a device that belongs to no storage", () => {
    render(<GuestDisksTable disks={[disk({ key: "hostpci0", storage: null })]} />);

    expect(within(rowOf("hostpci0")).getAllByText(FALLBACK).length).toBeGreaterThan(0);
  });

  it("says so plainly when the guest declares no volume", () => {
    render(<GuestDisksTable disks={[]} />);

    expect(screen.getByText(/aucun disque/i)).toBeInTheDocument();
    expect(screen.queryByRole("table")).toBeNull();
  });

  it("uses the hint it is given for an empty list", () => {
    render(<GuestDisksTable disks={[]} emptyHint="Configuration illisible." />);

    expect(screen.getByText("Configuration illisible.")).toBeInTheDocument();
  });

  it("declares its column headers and names itself", () => {
    render(<GuestDisksTable disks={[disk()]} />);

    const table = screen.getByRole("table");
    expect(table).toHaveAccessibleName(/Volumes déclarés/);
    for (const header of within(table).getAllByRole("columnheader")) {
      expect(header).toHaveAttribute("scope", "col");
    }
  });
});
