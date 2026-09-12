import { act, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { useClusterSeries, useNode } from "./useDetail";

vi.mock("@/api/client", async (importOriginal) => {
  const actual = await importOriginal<typeof import("./client")>();
  return { ...actual, fetchNode: vi.fn(), fetchClusterSeries: vi.fn() };
});

const { fetchClusterSeries, fetchNode } = await import("./client");
const fetchNodeMock = vi.mocked(fetchNode);
const fetchClusterSeriesMock = vi.mocked(fetchClusterSeries);

function Probe({ cluster, node }: { cluster: string; node: string }) {
  const { data, isLoading, isStale } = useNode(cluster, node);
  return (
    <dl>
      <dd data-testid="name">{data?.name ?? "—"}</dd>
      <dd data-testid="flags">
        {isLoading ? "loading" : ""}
        {isStale ? "stale" : ""}
      </dd>
    </dl>
  );
}

function nodePayload(name: string) {
  return { name, guests: [] } as unknown as Awaited<ReturnType<typeof fetchNode>>;
}

/**
 * Flushes pending promises and timers inside act().
 *
 * Wrapping the work in a helper keeps the await that React needs without
 * scattering empty async callbacks through the tests.
 */
async function settle(work: () => void = () => undefined): Promise<void> {
  await act(async () => {
    work();
    await Promise.resolve();
  });
}

beforeEach(() => {
  vi.useFakeTimers();
});

afterEach(() => {
  vi.useRealTimers();
  vi.clearAllMocks();
});

describe("useNode", () => {
  it("loads the node it is asked for", async () => {
    fetchNodeMock.mockResolvedValue(nodePayload("pve-01"));

    render(<Probe cluster="prod" node="pve-01" />);
    await settle();

    expect(screen.getByTestId("name")).toHaveTextContent("pve-01");
    expect(fetchNodeMock).toHaveBeenCalledWith("prod", "pve-01", expect.anything());
  });

  it("drops the previous node's data when the selection changes", async () => {
    // Showing pve-01's figures under pve-02's heading would be worse than
    // showing nothing, so a new subject starts from an empty state.
    fetchNodeMock.mockResolvedValue(nodePayload("pve-01"));
    const view = render(<Probe cluster="prod" node="pve-01" />);
    await settle();
    expect(screen.getByTestId("name")).toHaveTextContent("pve-01");

    let release: (value: Awaited<ReturnType<typeof fetchNode>>) => void = () => undefined;
    fetchNodeMock.mockImplementation(
      () =>
        new Promise((resolve) => {
          release = resolve;
        }),
    );
    view.rerender(<Probe cluster="prod" node="pve-02" />);

    // While the second request is in flight, nothing stale is on screen.
    expect(screen.getByTestId("name")).toHaveTextContent("—");
    expect(screen.getByTestId("flags")).toHaveTextContent("loading");

    await settle(() => {
      release(nodePayload("pve-02"));
    });
    expect(screen.getByTestId("name")).toHaveTextContent("pve-02");
  });

  it("keeps the last reading and flags it stale when a poll fails", async () => {
    fetchNodeMock.mockResolvedValue(nodePayload("pve-01"));
    render(<Probe cluster="prod" node="pve-01" />);
    await settle();

    fetchNodeMock.mockRejectedValue(new Error("boom"));
    await settle(() => {
      vi.advanceTimersByTime(5_000);
    });

    expect(screen.getByTestId("name")).toHaveTextContent("pve-01");
    expect(screen.getByTestId("flags")).toHaveTextContent("stale");
  });

  it("stops polling once unmounted", async () => {
    fetchNodeMock.mockResolvedValue(nodePayload("pve-01"));
    const view = render(<Probe cluster="prod" node="pve-01" />);
    await settle();
    const callsWhileMounted = fetchNodeMock.mock.calls.length;

    view.unmount();
    await settle(() => {
      vi.advanceTimersByTime(30_000);
    });

    expect(fetchNodeMock.mock.calls.length).toBe(callsWhileMounted);
  });
});

function SeriesProbe({ clusters }: { clusters: string[] }) {
  const { data, isStale } = useClusterSeries(clusters);
  return (
    <dl>
      <dd data-testid="ids">{Object.keys(data ?? {}).join(",") || "—"}</dd>
      <dd data-testid="flags">{isStale ? "stale" : ""}</dd>
    </dl>
  );
}

function seriesPayload(cluster: string) {
  return { cluster, timeframe: "hour", points: [], cpuAverage: 0 } as unknown as Awaited<
    ReturnType<typeof fetchClusterSeries>
  >;
}

describe("useClusterSeries", () => {
  it("asks every cluster for its hour", async () => {
    fetchClusterSeriesMock.mockImplementation((cluster: string) =>
      Promise.resolve(seriesPayload(cluster)),
    );

    render(<SeriesProbe clusters={["qual", "pprd"]} />);
    await settle();

    expect(screen.getByTestId("ids")).toHaveTextContent("qual,pprd");
    expect(fetchClusterSeriesMock).toHaveBeenCalledWith("qual", "hour", expect.anything());
  });

  it("drops the cluster that failed instead of the whole batch", async () => {
    // One unreachable cluster must not blank the charts of the others: the
    // overview keeps showing what it can, as it does for the cards themselves.
    fetchClusterSeriesMock.mockImplementation((cluster: string) =>
      cluster === "pprd"
        ? Promise.reject(new Error("upstream unavailable"))
        : Promise.resolve(seriesPayload(cluster)),
    );

    render(<SeriesProbe clusters={["qual", "pprd"]} />);
    await settle();

    expect(screen.getByTestId("ids")).toHaveTextContent("qual");
    expect(screen.getByTestId("flags")).not.toHaveTextContent("stale");
  });

  it("asks nothing when there is no cluster", async () => {
    render(<SeriesProbe clusters={[]} />);
    await settle();

    expect(fetchClusterSeriesMock).not.toHaveBeenCalled();
  });
});
