import { fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { ApiRequestError } from "@/api/client";
import type {
  GuestDetail,
  MaintenancePlan,
  NodeDetail,
  Series,
  Tasks,
  Thresholds,
} from "@/api/types";
import type { ResourceState } from "@/api/usePolledResource";
import guestFixture from "@/test/fixtures/guest.mock.json";
import guestTasksFixture from "@/test/fixtures/guest-tasks.mock.json";
import nodeFixture from "@/test/fixtures/node.mock.json";
import planFixture from "@/test/fixtures/plan.mock.json";
import seriesFixture from "@/test/fixtures/series.mock.json";

import { GuestRoute, NodeRoute } from "./DetailRoutes";

/**
 * One figure for the three resources, which is what the defaults are: a test
 * that needs them apart says so on the spot.
 */
function evenly(ratio: number): Thresholds {
  return { memory: ratio, cpu: ratio, storage: ratio };
}

/**
 * What these containers decide, and nothing else: which of the four states a
 * screen is in. The screens themselves are tested next door, and App's
 * integration test walks the whole path down from the tree — what is left, and
 * what used to be untested, is the branching: a first load, a hard failure, a
 * reading that survives a failed poll, and a subject that changes.
 */

vi.mock("@/api/useDetail", () => ({
  useNode: vi.fn(),
  useNodeSeries: vi.fn(),
  useGuest: vi.fn(),
  useGuestSeries: vi.fn(),
  useGuestTasks: vi.fn(),
  useMaintenancePlan: vi.fn(),
}));

const {
  useGuest,
  useGuestSeries,
  useGuestTasks,
  useNode,
  useMaintenancePlan,
  useNodeSeries,
} = await import("@/api/useDetail");

const nodeMock = vi.mocked(useNode);
const nodeSeriesMock = vi.mocked(useNodeSeries);
const guestMock = vi.mocked(useGuest);
const guestSeriesMock = vi.mocked(useGuestSeries);
const guestTasksMock = vi.mocked(useGuestTasks);
const planMock = vi.mocked(useMaintenancePlan);

const node = nodeFixture as unknown as NodeDetail;
const guest = guestFixture as unknown as GuestDetail;
const series = seriesFixture as unknown as Series;
const guestTasks = guestTasksFixture as unknown as Tasks;
const plan = planFixture as unknown as MaintenancePlan;

/** A resource in whichever of its states the test needs. */
function state<T>(patch: Partial<ResourceState<T>> = {}): ResourceState<T> {
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

function loaded<T>(data: T): ResourceState<T> {
  return state<T>({ data, lastUpdatedAt: new Date("2026-09-12T12:47:00Z") });
}

function failed<T>(status: number): ResourceState<T> {
  return state<T>({ error: new ApiRequestError("/api/x", status, null) });
}

function showNode(detail: ResourceState<NodeDetail>, onBack = vi.fn()) {
  nodeMock.mockReturnValue(detail);
  nodeSeriesMock.mockReturnValue(loaded(series));
  render(
    <NodeRoute
      cluster="qualification"
      clusterName="Qualification"
      thresholds={evenly(0.85)}
      node="prox-qual-2201-cit"
      onBackToOverview={onBack}
      onSelectGuest={vi.fn()}
    />,
  );
  return { onBack };
}

function showGuest(detail: ResourceState<GuestDetail>, onBack = vi.fn()) {
  guestMock.mockReturnValue(detail);
  guestSeriesMock.mockReturnValue(loaded(series));
  guestTasksMock.mockReturnValue(loaded(guestTasks));
  render(
    <GuestRoute
      cluster="qualification"
      clusterName="Qualification"
      thresholds={evenly(0.85)}
      onBackToOverview={onBack}
      vmid={guest.vmid}
    />,
  );
  return { onBack };
}

afterEach(() => {
  vi.clearAllMocks();
});

describe("NodeRoute", () => {
  it("shows the skeleton on the very first load", () => {
    showNode(state<NodeDetail>({ isLoading: true }));

    expect(screen.getByRole("status")).toHaveTextContent("Chargement…");
  });

  // The class of the failure decides the sentence: a 502 is the cluster, not
  // the daemon, and the operator must not be sent to check the wrong one.
  it("explains a failure that left nothing to show", () => {
    showNode(failed<NodeDetail>(502));

    const alert = screen.getByRole("alert");
    expect(alert).toHaveTextContent("Cluster injoignable");
    expect(screen.queryByRole("status")).toBeNull();
  });

  it("retries from the error view", () => {
    const refresh = vi.fn();
    showNode(state<NodeDetail>({ error: new Error("boom"), refresh }));

    fireEvent.click(screen.getByRole("button", { name: "Réessayer" }));

    expect(refresh).toHaveBeenCalledTimes(1);
  });

  // Only a 404 offers it: what is deleted will not come back by asking again,
  // while the overview still lists what the cluster holds.
  it("offers the way back for a node that is gone, and only then", () => {
    const { onBack } = showNode(failed<NodeDetail>(404));

    fireEvent.click(
      screen.getByRole("button", { name: "Retour à la vue d’ensemble" }),
    );
    expect(onBack).toHaveBeenCalledTimes(1);
  });

  it("does not offer it for a cluster that is merely unreachable", () => {
    showNode(failed<NodeDetail>(502));

    expect(
      screen.queryByRole("button", { name: "Retour à la vue d’ensemble" }),
    ).toBeNull();
  });

  // The whole point of isStale: the last good reading stays on screen, with a
  // banner saying when it was taken.
  it("keeps the reading and says it is old when a poll fails", () => {
    showNode({ ...loaded(node), isStale: true, error: new Error("boom") });

    expect(screen.getByText(node.name)).toBeInTheDocument();
    expect(screen.getByText(/connexion perdue/)).toBeInTheDocument();
  });

  it("draws no banner over a fresh reading", () => {
    showNode(loaded(node));

    expect(screen.queryByText(/connexion perdue/)).toBeNull();
    expect(screen.getByText(node.name)).toBeInTheDocument();
  });

  // A failing series must not take the screen down: the metrics above it are
  // still worth reading.
  it("renders the node even when its history could not be read", () => {
    nodeMock.mockReturnValue(loaded(node));
    nodeSeriesMock.mockReturnValue(failed<Series>(500));
    render(
      <NodeRoute
        cluster="qualification"
        clusterName="Qualification"
        thresholds={evenly(0.85)}
        node={node.name}
        onBackToOverview={vi.fn()}
        onSelectGuest={vi.fn()}
      />,
    );

    expect(screen.getByText(node.name)).toBeInTheDocument();
    expect(screen.queryByRole("alert")).toBeNull();
  });

  // Changing the selection changes the subject: the previous node's figures
  // under the new heading would be worse than showing nothing, so the screen
  // goes back to the skeleton until the new answer lands.
  it("returns to the skeleton when another node is selected", () => {
    nodeSeriesMock.mockReturnValue(loaded(series));
    nodeMock.mockImplementation((_cluster: string, name: string) =>
      name === node.name ? loaded(node) : state<NodeDetail>({ isLoading: true }),
    );
    const { rerender } = render(
      <NodeRoute
        cluster="qualification"
        clusterName="Qualification"
        thresholds={evenly(0.85)}
        node={node.name}
        onBackToOverview={vi.fn()}
        onSelectGuest={vi.fn()}
      />,
    );
    expect(screen.getByText(node.name)).toBeInTheDocument();

    rerender(
      <NodeRoute
        cluster="qualification"
        clusterName="Qualification"
        thresholds={evenly(0.85)}
        node="prox-qual-2202-cit"
        onBackToOverview={vi.fn()}
        onSelectGuest={vi.fn()}
      />,
    );

    expect(screen.getByRole("status")).toHaveTextContent("Chargement…");
    expect(screen.queryByText(node.name)).toBeNull();
  });

  it("opens the maintenance plan on demand, and not before", () => {
    planMock.mockReturnValue(loaded(plan));
    showNode(loaded(node));
    expect(screen.queryByRole("dialog")).toBeNull();

    fireEvent.click(screen.getByRole("button", { name: /maintenance/i }));

    expect(screen.getByRole("dialog")).toBeInTheDocument();
  });
});

describe("GuestRoute", () => {
  it("shows the skeleton on the very first load", () => {
    showGuest(state<GuestDetail>({ isLoading: true }));

    expect(screen.getByRole("status")).toHaveTextContent("Chargement…");
  });

  it("names the guest that vanished rather than accusing moxyd", () => {
    const { onBack } = showGuest(failed<GuestDetail>(404));

    expect(screen.getByRole("alert")).toHaveTextContent("Objet introuvable");
    fireEvent.click(
      screen.getByRole("button", { name: "Retour à la vue d’ensemble" }),
    );
    expect(onBack).toHaveBeenCalledTimes(1);
  });

  it("keeps the reading and says it is old when a poll fails", () => {
    showGuest({ ...loaded(guest), isStale: true, error: new Error("boom") });

    expect(screen.getByText(/connexion perdue/)).toBeInTheDocument();
    expect(screen.getAllByText(guest.name).length).toBeGreaterThan(0);
  });

  // A failing log is no reason to blank the screen either: the metrics above
  // it still say what the machine is doing.
  it("renders the guest even when its log could not be read", () => {
    guestMock.mockReturnValue(loaded(guest));
    guestSeriesMock.mockReturnValue(loaded(series));
    guestTasksMock.mockReturnValue(failed<Tasks>(500));
    render(
      <GuestRoute
        cluster="qualification"
        clusterName="Qualification"
        thresholds={evenly(0.85)}
        onBackToOverview={vi.fn()}
        vmid={guest.vmid}
      />,
    );

    expect(screen.getAllByText(guest.name).length).toBeGreaterThan(0);
    expect(screen.queryByRole("alert")).toBeNull();
  });
});
