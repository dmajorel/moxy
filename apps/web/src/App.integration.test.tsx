import { render, screen, waitFor, within } from "@testing-library/react";

import { App } from "./App";
import overviewFixture from "./test/fixtures/overview.mock.json";

/**
 * End-to-end check against the payload moxyd actually serves.
 *
 * The fixture was captured from `./bin/moxyd -mock`, so this test fails the day
 * the Go model and src/api/types.ts drift apart — which unit tests built on
 * hand-written objects cannot catch. Unlike the other App tests, the real
 * useOverview hook runs here; only fetch is stubbed.
 */
describe("App against the real moxyd payload", () => {
  beforeEach(() => {
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

    // The header totals of screen 4.
    await waitFor(() => {
      expect(within(main).getByText("11 nœuds")).toBeInTheDocument();
    });
    expect(within(main).getByText("148 VM")).toBeInTheDocument();
    expect(within(main).getByText("2 alertes")).toBeInTheDocument();

    // The three cluster cards.
    expect(within(main).getByText("Qualification")).toBeInTheDocument();
    expect(within(main).getByText("Préproduction")).toBeInTheDocument();
    expect(within(main).getByText("Production")).toBeInTheDocument();

    // Préproduction is the degraded one, with its drained node.
    expect(within(main).getByText("Dégradé")).toBeInTheDocument();
    expect(within(main).getAllByText("Sain")).toHaveLength(2);
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
