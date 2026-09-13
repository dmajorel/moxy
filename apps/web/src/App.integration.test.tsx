import { render, screen, waitFor, within } from "@testing-library/react";

import { App } from "./App";
import overviewFixture from "./test/fixtures/overview.mock.json";

/**
 * End-to-end check against the payload moxyd actually serves.
 *
 * The fixture is GENERATED from the mock by
 * apps/api/internal/server/fixtures_test.go, on a pinned clock, so this test
 * fails the day the Go model and src/api/types.ts drift apart — which unit
 * tests built on hand-written objects cannot catch. It used to be captured by
 * hand, which meant it only failed when someone remembered to re-capture it;
 * it had drifted a whole cluster behind. Unlike the other App tests, the real
 * useOverview hook runs here; only fetch is stubbed.
 */
describe("App against the real moxyd payload", () => {
  beforeEach(() => {
    // The screen is derived from the address bar, and jsdom's history is
    // shared by every test in this file.
    window.history.replaceState(null, "", "/");

    vi.stubGlobal(
      "fetch",
      vi.fn(() =>
        Promise.resolve(
          new Response(JSON.stringify(overviewFixture), {
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

  it("paints the figures of the reference mockup", async () => {
    render(<App />);

    const main = await screen.findByRole("main");

    // The header totals, read from the fixture rather than written out: the
    // sample data grows a cluster now and then, and a number copied here would
    // make this test fail for a reason that has nothing to do with the app.
    const { totals } = overviewFixture;
    await waitFor(() => {
      expect(within(main).getByText(`${String(totals.nodes)} nœuds`)).toBeInTheDocument();
    });
    expect(within(main).getByText(`${String(totals.vms)} VM`)).toBeInTheDocument();
    expect(within(main).getByText(`${String(totals.alerts)} alertes`)).toBeInTheDocument();

    // One card per cluster, each under its own name.
    for (const cluster of overviewFixture.clusters) {
      expect(within(main).getByText(cluster.name)).toBeInTheDocument();
    }

    // The three verdicts the sample exercises, each rendered: a healthy
    // cluster, the degraded one with its drained node, and the one that could
    // not be read at all.
    expect(within(main).getByText("Dégradé")).toBeInTheDocument();
    expect(within(main).getByText("Injoignable")).toBeInTheDocument();
    const healthy = overviewFixture.clusters.filter((c) => c.status === "healthy");
    expect(within(main).getAllByText("Sain")).toHaveLength(healthy.length);
  });

  it("lists the guests of a node in the tree, with truncated labels", async () => {
    render(<App />);

    const tree = await screen.findByRole("tree");
    await waitFor(() => {
      expect(within(tree).getByText("Qualification")).toBeInTheDocument();
    });

    // Every guest label keeps its id and drops the boilerplate: the long raw
    // name from the naming convention must never reach the DOM.
    expect(within(tree).queryByText(/^sli-.*-qul$/)).not.toBeInTheDocument();
  });

  it("shows no stale banner while polling succeeds", async () => {
    render(<App />);

    await screen.findByRole("main");
    expect(screen.queryByText(/connexion perdue/i)).not.toBeInTheDocument();
  });
});
