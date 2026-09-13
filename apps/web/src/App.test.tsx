import { fireEvent, render, screen, within } from "@testing-library/react";

import type { ClusterOverview, Node, Overview } from "@/api/types";
import { useHealth } from "@/api/useHealth";
import type { OverviewState } from "@/api/useOverview";
import { useOverview } from "@/api/useOverview";

import { App } from "./App";

// The polling hook is the only input of the shell; mocking it lets every state
// be rendered without a fake server.
vi.mock("@/api/useOverview", () => ({
  useOverview: vi.fn(),
}));

// Same reason, and it keeps the one-shot /healthz request out of every test in
// this file: the version is a label the bar reads, not a state it holds.
vi.mock("@/api/useHealth", () => ({
  useHealth: vi.fn(),
}));

const useOverviewMock = vi.mocked(useOverview);
const useHealthMock = vi.mocked(useHealth);

beforeEach(() => {
  useHealthMock.mockReturnValue({ status: "ok", version: "0862b0c" });
});

function node(name: string, status: Node["status"] = "online"): Node {
  return {
    name,
    status,
    uptime: 3_542_400,
    cpu: { ratio: 0.04, cores: 32 },
    memory: { used: 21_474_836_480, total: 137_438_953_472, ratio: 0.15625 },
    pendingUpdates: null,
    guests: [],
  };
}

function cluster(id: string, name: string, patch: Partial<ClusterOverview> = {}): ClusterOverview {
  return {
    id,
    name,
    color: null,
    status: "healthy",
    fetchedAt: "2026-09-12T14:32:00Z",
    error: null,
    quorum: { quorate: true, nodes: 3, online: 3 },
    cpu: { ratio: 0.04, cores: 96 },
    memory: { used: 65_498_251_264, total: 412_316_860_416, ratio: 0.1589 },
    storage: { used: 1_319_413_953_331, total: 5_937_362_789_990, ratio: 0.2222 },
    vms: { running: 13, stopped: 0, templates: 1, total: 13 },
    nodes: [node(`${id}-2201`), node(`${id}-2202`), node(`${id}-2203`)],
    updates: null,
    alerts: [],
    ...patch,
  };
}

const overview: Overview = {
  generatedAt: "2026-09-12T14:32:00Z",
  thresholds: { memory: 0.8 },
  totals: { clusters: 2, nodes: 6, nodesOnline: 6, vms: 59, alerts: 1 },
  clusters: [
    cluster("qual", "Qualification"),
    cluster("pprd", "Préproduction", {
      status: "degraded",
      vms: { running: 44, stopped: 2, templates: 0, total: 46 },
      alerts: [{ kind: "memory_high", ratio: 0.828 }],
    }),
  ],
};

function state(patch: Partial<OverviewState> = {}): OverviewState {
  return {
    data: null,
    error: null,
    isLoading: false,
    isStale: false,
    lastUpdatedAt: null,
    refresh: vi.fn(),
    ...patch,
  };
}

describe("App", () => {
  it("shows the loading view until the first answer arrives", () => {
    useOverviewMock.mockReturnValue(state({ isLoading: true }));

    render(<App />);

    expect(screen.getByRole("status")).toBeInTheDocument();
    // The tree renders straight away but has nothing to show yet.
    expect(screen.queryByRole("treeitem")).not.toBeInTheDocument();
  });

  it("renders the top bar, the tree and the overview once data is in", () => {
    useOverviewMock.mockReturnValue(
      state({ data: overview, lastUpdatedAt: new Date("2026-09-12T14:32:00Z") }),
    );

    render(<App />);

    expect(screen.getByRole("banner")).toBeInTheDocument();
    expect(screen.getByRole("tree")).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "Clusters" })).toBeInTheDocument();
    // The name shows in the tree and on its card, so scope the assertions.
    const tree = screen.getByRole("tree");
    expect(within(tree).getByText("Qualification")).toBeInTheDocument();
    expect(within(screen.getByRole("main")).getByText("Qualification")).toBeInTheDocument();
  });

  it("shows the version /healthz reports next to the wordmark", () => {
    useOverviewMock.mockReturnValue(state({ data: overview }));

    render(<App />);

    expect(within(screen.getByRole("banner")).getByText("0862b0c")).toBeInTheDocument();
  });

  it("shows the wordmark alone when /healthz never answered", () => {
    useHealthMock.mockReturnValue(null);
    useOverviewMock.mockReturnValue(state({ data: overview }));

    render(<App />);

    const banner = screen.getByRole("banner");
    expect(within(banner).getByText("moxy")).toBeInTheDocument();
    expect(within(banner).queryByText("0862b0c")).not.toBeInTheDocument();
  });

  it("falls back to the error view when nothing could ever be fetched", () => {
    const refresh = vi.fn();
    useOverviewMock.mockReturnValue(
      state({ error: new Error("overview unavailable"), refresh }),
    );

    render(<App />);

    fireEvent.click(screen.getByRole("button", { name: "Réessayer" }));
    expect(refresh).toHaveBeenCalledTimes(1);
  });

  it("keeps showing the data and adds a banner when the last poll failed", () => {
    useOverviewMock.mockReturnValue(
      state({
        data: overview,
        error: new Error("dial tcp: no such host"),
        isStale: true,
        lastUpdatedAt: new Date("2026-09-12T14:32:00Z"),
      }),
    );

    render(<App />);

    // The data must survive the failure, which is the whole point of the
    // backend serving its last known snapshot.
    expect(screen.getByRole("heading", { name: "Clusters" })).toBeInTheDocument();
    expect(screen.getByText(/connexion perdue/i)).toBeInTheDocument();
    // The English backend message is for diagnosis, never for the user.
    expect(screen.queryByText(/no such host/)).not.toBeInTheDocument();
  });

  it("narrows the view to one cluster when the tree selects it", () => {
    useOverviewMock.mockReturnValue(state({ data: overview }));

    render(<App />);
    expect(screen.getByText("2 clusters")).toBeInTheDocument();

    const tree = screen.getByRole("tree");
    fireEvent.click(within(tree).getByText("Préproduction"));

    expect(within(tree).getByText("Qualification")).toBeInTheDocument(); // still listed
    const main = screen.getByRole("main");
    expect(within(main).queryByText("Qualification")).not.toBeInTheDocument();
    // Header figures describe what is on screen, not the whole estate.
    expect(within(main).getByText("46 VM")).toBeInTheDocument();
  });

  it("returns to every cluster from the top bar switcher", () => {
    useOverviewMock.mockReturnValue(state({ data: overview }));

    render(<App />);

    const tree = screen.getByRole("tree");
    fireEvent.click(within(tree).getByText("Préproduction"));
    expect(within(screen.getByRole("main")).getByText("46 VM")).toBeInTheDocument();

    const banner = screen.getByRole("banner");
    fireEvent.click(within(banner).getByRole("button", { name: /Préproduction/ }));
    fireEvent.click(screen.getByRole("menuitemradio", { name: /Tous les clusters/ }));

    expect(within(screen.getByRole("main")).getByText("59 VM")).toBeInTheDocument();
  });

  it("shows an empty state rather than an empty grid", () => {
    useOverviewMock.mockReturnValue(
      state({ data: { ...overview, totals: { ...overview.totals, clusters: 0 }, clusters: [] } }),
    );

    render(<App />);

    expect(screen.getByText("Aucun cluster à afficher")).toBeInTheDocument();
  });
});
