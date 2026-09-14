import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { axe } from "vitest-axe";

import { applyTheme } from "@/lib/theme";
import type { ThemePreference } from "@/lib/theme";

import { App } from "./App";
import guestFixture from "./test/fixtures/guest.mock.json";
import guestTasksFixture from "./test/fixtures/guest-tasks.mock.json";
import nodeFixture from "./test/fixtures/node.mock.json";
import overviewFixture from "./test/fixtures/overview.mock.json";
import seriesFixture from "./test/fixtures/series.mock.json";
import tasksFixture from "./test/fixtures/tasks.mock.json";

/**
 * axe over the whole application, on the payloads `moxyd -mock` serves.
 *
 * The lint rules of jsx-a11y read the source and catch what is wrong in a file;
 * this reads the rendered tree and catches what is only wrong once assembled —
 * a card turned into a `role="button"` with no accessible name, a status dot
 * carrying its meaning in colour alone. The two are not exclusive, and neither
 * subsumes the other.
 *
 * `color-contrast` is switched off here and nowhere else: jsdom loads no
 * stylesheet, so every element would be judged black on transparent. The
 * palette is measured against tokens.css itself, with the normative WCAG
 * arithmetic, in styles/tokens.test.ts.
 *
 * The matcher itself is registered for the whole suite by test/setup.ts.
 */

const AXE_OPTIONS = { rules: { "color-contrast": { enabled: false } } };

/** Request objects stringify to "[object Object]", so pick the url out. */
function urlOf(input: RequestInfo | URL): string {
  if (typeof input === "string") return input;
  if (input instanceof URL) return input.toString();
  return input.url;
}

function routeFor(url: string): unknown {
  if (url === "/healthz") return { status: "ok", version: "0862b0c" };
  if (url === "/api/overview") return overviewFixture;
  if (url.includes("/rrd")) return seriesFixture;
  if (url.includes("/guests/") && url.includes("/tasks")) return guestTasksFixture;
  if (url.includes("/tasks")) return tasksFixture;
  if (url.includes("/guests/")) return guestFixture;
  if (url.includes("/nodes/")) return nodeFixture;
  throw new Error(`unexpected request: ${url}`);
}

beforeEach(() => {
  window.history.replaceState(null, "", "/");
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
  document.documentElement.removeAttribute("data-theme");
});

/** Expands the first cluster so its nodes and guests are reachable. */
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

async function expectNoViolations(container: HTMLElement) {
  const results = await axe(container, AXE_OPTIONS);
  expect(results).toHaveNoViolations();
}

/**
 * Both themes, because the attribute is on the root element and a rule that
 * reads it would see a different tree — and because a theme that only half
 * applies is exactly the kind of regression nothing else here would notice.
 */
const THEMES: ThemePreference[] = ["light", "dark"];

describe.each(THEMES)("the application in %s theme", (theme) => {
  beforeEach(() => {
    applyTheme(theme);
  });

  it("has no accessibility violation on the overview", async () => {
    const { container } = render(<App />);
    await screen.findByRole("tree");
    await waitFor(() => {
      expect(screen.getAllByRole("article").length).toBeGreaterThan(0);
    });

    await expectNoViolations(container);
  });

  // One cluster selected: the same cards, plus the journal that only belongs
  // to a single cluster.
  it("has no accessibility violation on a cluster", async () => {
    const { container } = render(<App />);
    const main = screen.getByRole("main");
    await openCluster();

    await waitFor(() => {
      expect(
        within(main).getByRole("heading", { name: "Journal du cluster" }),
      ).toBeInTheDocument();
    });
    await waitFor(() => {
      // Named, because the cluster cards on the same screen are tables too.
      expect(
        within(main).getByRole("table", { name: /Tâches récentes/ }),
      ).toBeInTheDocument();
    });

    await expectNoViolations(container);
  });

  it("has no accessibility violation on a node", async () => {
    const { container } = render(<App />);
    const tree = await openCluster();

    fireEvent.click(within(tree).getByText("prox-qual-2201-cit"));
    const main = screen.getByRole("main");
    await waitFor(() => {
      expect(within(main).getByRole("heading", { level: 1 })).toHaveTextContent(
        "prox-qual-2201-cit",
      );
    });

    await expectNoViolations(container);
  });

  it("has no accessibility violation on a guest", async () => {
    const { container } = render(<App />);
    const tree = await openCluster();

    fireEvent.click(within(tree).getByText("prox-qual-2201-cit"));
    await waitFor(() => {
      expect(within(tree).getByText(guestFixture.name)).toBeInTheDocument();
    });
    fireEvent.click(within(tree).getByText(guestFixture.name));

    const main = screen.getByRole("main");
    await waitFor(() => {
      expect(within(main).getByRole("heading", { level: 1 })).toHaveTextContent(
        guestFixture.name,
      );
    });

    await expectNoViolations(container);
  });
});

/**
 * The token prompt, which replaces the whole application: there is no shell
 * around it to carry a landmark, a heading or a label, so everything it needs
 * it has to bring itself.
 */
describe.each(THEMES)("the token prompt in %s theme", (theme) => {
  beforeEach(() => {
    applyTheme(theme);
    // moxyd is in `auth.mode: "token"` and has been told nothing yet.
    vi.stubGlobal(
      "fetch",
      vi.fn(() =>
        Promise.resolve(
          new Response(JSON.stringify({ error: "unauthorized" }), {
            status: 401,
            headers: { "Content-Type": "application/json" },
          }),
        ),
      ),
    );
  });

  it("has no accessibility violation", async () => {
    const { container } = render(<App />);
    await screen.findByLabelText("Jeton d\u2019acc\u00e8s");

    await expectNoViolations(container);
  });
});
