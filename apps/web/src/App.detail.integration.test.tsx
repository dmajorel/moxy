import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { App } from "./App";
import guestFixture from "./test/fixtures/guest.mock.json";
import guestTasksFixture from "./test/fixtures/guest-tasks.mock.json";
import nodeFixture from "./test/fixtures/node.mock.json";
import nodeUpdatesFixture from "./test/fixtures/node-updates.mock.json";
import overviewFixture from "./test/fixtures/overview.mock.json";
import seriesFixture from "./test/fixtures/series.mock.json";
import tasksFixture from "./test/fixtures/tasks.mock.json";

/**
 * End-to-end check of the detail screens against payloads captured from
 * `moxyd -mock`.
 *
 * The real hooks run; only fetch is stubbed, and it routes by path exactly as
 * the dev proxy does. The day internal/detail/model.go and src/api/types.ts
 * drift apart, this test fails — which no test built on hand-written objects
 * could catch.
 */
/** Request objects stringify to "[object Object]", so pick the url out. */
function urlOf(input: RequestInfo | URL): string {
  if (typeof input === "string") return input;
  if (input instanceof URL) return input.toString();
  return input.url;
}

function routeFor(url: string): unknown {
  if (url === "/api/overview") return overviewFixture;
  if (url.includes("/rrd")) return seriesFixture;
  // Checked before the cluster journal: a guest reads its own log, from its
  // hosting node, and the two answers are different documents.
  if (url.includes("/guests/") && url.includes("/tasks")) return guestTasksFixture;
  if (url.includes("/tasks")) return tasksFixture;
  if (url.includes("/guests/")) return guestFixture;
  // Two node payloads: a qualification node whose token may not ask about
  // updates, and a production one that lists twelve pending packages.
  if (url.includes("/nodes/prox-prod-")) return nodeUpdatesFixture;
  if (url.includes("/nodes/")) return nodeFixture;
  throw new Error(`unexpected request: ${url}`);
}

beforeEach(() => {
  vi.stubGlobal(
    "fetch",
    vi.fn((input: RequestInfo | URL) =>
      Promise.resolve(
        new Response(JSON.stringify(routeFor(urlOf(input))), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        }),
      ),
    ),
  );
});

afterEach(() => {
  vi.unstubAllGlobals();
});

/**
 * Expands the first cluster so its nodes are reachable.
 *
 * The tree only opens the ancestors of the current selection, and the default
 * view is "every cluster", so everything starts collapsed.
 */
async function openCluster() {
  const tree = await screen.findByRole("tree");
  await waitFor(() => {
    expect(within(tree).getByText("Qualification")).toBeInTheDocument();
  });
  fireEvent.click(within(tree).getByText("Qualification"));
  await waitFor(() => {
    expect(within(tree).getByText("prox-qual-2201-cit")).toBeInTheDocument();
  });
  return tree;
}

describe("detail screens against the real moxyd payloads", () => {
  it("opens the node view from the tree", async () => {
    render(<App />);
    const tree = await openCluster();

    fireEvent.click(within(tree).getByText("prox-qual-2201-cit"));

    const main = screen.getByRole("main");
    await waitFor(() => {
      expect(
        within(main).getByRole("heading", { level: 1, name: "prox-qual-2201-cit" }),
      ).toBeInTheDocument();
    });

    // The figures the backend actually serves, rendered through lib/format.
    expect(within(main).getByText("Load average")).toBeInTheDocument();
    expect(within(main).getByText("Stockage local")).toBeInTheDocument();
    expect(
      within(main).getByText("Machines virtuelles sur ce nœud"),
    ).toBeInTheDocument();
    // The chart is drawn, not a placeholder: the series fixture has samples.
    expect(within(main).getByRole("img", { name: /Charge CPU/ })).toBeInTheDocument();
  });

  it("opens a guest view and narrows the journal to it", async () => {
    render(<App />);
    const tree = await openCluster();

    fireEvent.click(within(tree).getByText("prox-qual-2201-cit"));
    const main = screen.getByRole("main");
    await waitFor(() => {
      expect(within(main).getByRole("heading", { level: 1 })).toHaveTextContent(
        "prox-qual-2201-cit",
      );
    });

    // Guest rows carry the full name PVE reports, and never the vmid.
    const guestRow = within(tree).getByText(String(guestFixture.name));
    fireEvent.click(guestRow);

    await waitFor(() => {
      expect(within(main).getByRole("heading", { level: 1 })).toHaveTextContent(
        String(guestFixture.name),
      );
    });
    expect(within(main).getByText("Disque de boot")).toBeInTheDocument();
    expect(within(main).getByText("Tâches récentes")).toBeInTheDocument();

    // The list comes from the guest's own route, not from the cluster journal
    // sieved by vmid: this snapshot is nowhere in that journal, and would be
    // missing from the view the day the cluster is busy enough to push a
    // machine's lines out of its last twenty-five entries.
    expect(within(main).getByText("Instantané · 100")).toBeInTheDocument();
    expect(within(main).getByText(/sur prox-qual-2201-cit/)).toBeInTheDocument();
  });

  it("lists the pending packages the backend serves", async () => {
    render(<App />);
    const tree = await screen.findByRole("tree");
    await waitFor(() => {
      expect(within(tree).getByText("Production")).toBeInTheDocument();
    });
    fireEvent.click(within(tree).getByText("Production"));
    await waitFor(() => {
      expect(within(tree).getByText("prox-prod-2401-cit")).toBeInTheDocument();
    });
    fireEvent.click(within(tree).getByText("prox-prod-2401-cit"));

    const main = screen.getByRole("main");
    await waitFor(() => {
      expect(
        within(main).getByRole("heading", { level: 1, name: "prox-prod-2401-cit" }),
      ).toBeInTheDocument();
    });

    const first = nodeUpdatesFixture.updates[0];
    expect(first).toBeDefined();
    expect(
      within(main).getByRole("heading", { name: "Mises à jour en attente" }),
    ).toBeInTheDocument();
    expect(
      within(main).getByText(`${String(nodeUpdatesFixture.updates.length)} paquets`),
    ).toBeInTheDocument();
    const row = within(main).getByText(String(first?.package)).closest("tr");
    expect(row).not.toBeNull();
    expect(
      within(row as HTMLElement).getByText(
        `${String(first?.oldVersion)} → ${String(first?.version)}`,
      ),
    ).toBeInTheDocument();
  });

  it("names guests in the sidebar as PVE does, without their vmid", async () => {
    render(<App />);
    const tree = await openCluster();

    fireEvent.click(within(tree).getByText("prox-qual-2201-cit"));
    await waitFor(() => {
      expect(
        within(tree).getByText(String(nodeFixture.guests[0]?.name)),
      ).toBeInTheDocument();
    });

    expect(within(tree).queryByText(/^\d+ · /)).toBeNull();
  });
});
