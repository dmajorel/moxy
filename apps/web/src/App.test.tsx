import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";

import { ApiRequestError } from "@/api/client";
import { LANG_ATTRIBUTE, LANG_STORAGE_KEY } from "@/lib/lang";
import { stubLanguages } from "@/test/stubs";
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
  // The screen is derived from the address bar now, and jsdom's history is
  // shared by every test in the file: one that navigated would decide where
  // the next one starts.
  window.history.replaceState(null, "", "/");
  document.title = "moxy";
  window.localStorage.removeItem(LANG_STORAGE_KEY);
  document.documentElement.removeAttribute(LANG_ATTRIBUTE);
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
  thresholds: { memory: 0.8, cpu: 0.8, storage: 0.8 },
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

  // moxyd is configured with `auth.mode: "token"` and is waiting to be told
  // the shared token. There is nothing to navigate behind that, so the prompt
  // replaces the application rather than sitting inside it.
  it("asks for the token instead of the shell when moxyd answers 401", () => {
    const refresh = vi.fn();
    useOverviewMock.mockReturnValue(
      state({ error: new ApiRequestError("/api/overview", 401, "unauthorized"), refresh }),
    );

    render(<App />);

    expect(
      screen.getByRole("heading", { name: "Authentification requise" }),
    ).toBeInTheDocument();
    expect(screen.queryByRole("tree")).not.toBeInTheDocument();
    expect(screen.queryByRole("banner")).not.toBeInTheDocument();
  });

  // The rule that a failed poll never clears the data is about a cluster that
  // has gone quiet, not about a daemon that has stopped answering this browser
  // at all: readings nobody is authorized to see any more must not stay up.
  it("takes the readings off the screen when the session is refused", () => {
    useOverviewMock.mockReturnValue(
      state({
        data: overview,
        error: new ApiRequestError("/api/overview", 401, "unauthorized"),
        isStale: true,
      }),
    );

    render(<App />);

    expect(screen.queryByText("Qualification")).not.toBeInTheDocument();
    expect(screen.getByLabelText("Jeton d’accès")).toBeInTheDocument();
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

describe("the URL is the selection", () => {
  // A supervision UI that cannot be linked to is a single-player tool. An
  // operator opening a node during an incident has to be able to refresh, to
  // paste the address into an on-call channel, and to go back.
  it("opens the object the address bar names", () => {
    useOverviewMock.mockReturnValue(state({ data: overview }));
    window.history.replaceState(null, "", "/clusters/pprd");

    render(<App />);

    const main = screen.getByRole("main");
    expect(within(main).getByText("46 VM")).toBeInTheDocument();
    expect(within(main).queryByText("Qualification")).not.toBeInTheDocument();
  });

  it("writes the selection into the address bar", () => {
    useOverviewMock.mockReturnValue(state({ data: overview }));

    render(<App />);
    fireEvent.click(within(screen.getByRole("tree")).getByText("Préproduction"));

    expect(window.location.pathname).toBe("/clusters/pprd");
  });

  // A node name is operator data: one with a space in it must survive the
  // round trip through the address bar.
  it("encodes a node name into the path and reads it back", () => {
    const withSpace = cluster("qual", "Qualification", {
      nodes: [node("prox qual 2201")],
    });
    useOverviewMock.mockReturnValue(
      state({ data: { ...overview, clusters: [withSpace] } }),
    );
    window.history.replaceState(null, "", "/clusters/qual/nodes/prox%20qual%202201");

    render(<App />);

    // The detail screen is reached, which is all this asserts: its own data
    // come from a hook this file does not stub, so it shows its loading state.
    expect(window.location.pathname).toBe("/clusters/qual/nodes/prox%20qual%202201");
    expect(screen.queryByText("Aucun cluster à afficher")).not.toBeInTheDocument();
  });

  it("goes back where the operator was", async () => {
    useOverviewMock.mockReturnValue(state({ data: overview }));

    render(<App />);
    fireEvent.click(within(screen.getByRole("tree")).getByText("Préproduction"));
    expect(window.location.pathname).toBe("/clusters/pprd");

    // jsdom implements back() asynchronously, through the popstate event the
    // hook is subscribed to -- which is the whole mechanism under test.
    window.history.back();
    await waitFor(() => {
      expect(window.location.pathname).toBe("/");
    });
    await waitFor(() => {
      expect(within(screen.getByRole("main")).getByText("59 VM")).toBeInTheDocument();
    });
  });

  // Narrowing the filter refines the view one is already on; pushing it would
  // make the back button undo a menu choice one click at a time.
  it("replaces rather than pushes when the cluster filter changes", () => {
    useOverviewMock.mockReturnValue(state({ data: overview }));

    render(<App />);
    const before = window.history.length;

    const banner = screen.getByRole("banner");
    fireEvent.click(within(banner).getByRole("button", { name: /2 clusters/ }));
    fireEvent.click(screen.getByRole("menuitemradio", { name: /Préproduction/ }));

    expect(window.location.pathname).toBe("/clusters/pprd");
    expect(window.history.length).toBe(before);
  });

  it("says so when the address designates nothing", () => {
    useOverviewMock.mockReturnValue(state({ data: overview }));
    window.history.replaceState(null, "", "/clusters/prod/guests/not-a-vmid");

    render(<App />);

    expect(screen.getByText("Objet introuvable")).toBeInTheDocument();
    // Not the generic failure: nothing failed, the address is wrong.
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });

  // The message does not wait on the overview: nothing about it depends on
  // data, and a spinner before bad news only delays the bad news.
  it("says so before the first poll has answered", () => {
    useOverviewMock.mockReturnValue(state({ isLoading: true }));
    window.history.replaceState(null, "", "/nowhere");

    render(<App />);

    expect(screen.getByText("Objet introuvable")).toBeInTheDocument();
    expect(screen.queryByRole("status")).not.toBeInTheDocument();
  });
});

describe("the tab title", () => {
  // Ten tabs all called "moxy" are ten tabs nobody can tell apart, which is
  // exactly the state an incident leaves them in.
  it.each([
    ["/", "Clusters · moxy"],
    ["/clusters/pprd", "Préproduction · moxy"],
    ["/clusters/pprd/nodes/pprd-2201", "pprd-2201 · Préproduction · moxy"],
    ["/clusters/pprd/guests/103", "VM 103 · Préproduction · moxy"],
    ["/nowhere", "Objet introuvable · moxy"],
  ])("is %s → %s", (path, want) => {
    useOverviewMock.mockReturnValue(state({ data: overview }));
    window.history.replaceState(null, "", path);

    render(<App />);

    expect(document.title).toBe(want);
  });

  // The cluster id stands in until the overview names it: a title that waited
  // would show "moxy" for the first second of every load.
  it("falls back to the cluster id before the overview arrives", () => {
    useOverviewMock.mockReturnValue(state({ isLoading: true }));
    window.history.replaceState(null, "", "/clusters/pprd");

    render(<App />);

    expect(document.title).toBe("pprd · moxy");
  });
});

// The ⌘K field carried its query up to App and was read by nothing: an
// operator typed "pprd-2302", nothing happened, and concluded the tool was
// broken.
describe("the global search", () => {
  function searchField(): HTMLElement {
    return screen.getByRole("searchbox", { name: "Recherche globale" });
  }

  it("filters the tree as it is typed", () => {
    useOverviewMock.mockReturnValue(state({ data: overview }));

    render(<App />);
    const tree = screen.getByRole("tree");
    expect(within(tree).getByText("Qualification")).toBeInTheDocument();

    fireEvent.change(searchField(), { target: { value: "pprd" } });

    expect(within(tree).queryByText("Qualification")).toBeNull();
    expect(within(tree).getByText("Préproduction")).toBeInTheDocument();
  });

  it("opens the first result on Enter", () => {
    useOverviewMock.mockReturnValue(state({ data: overview }));

    render(<App />);
    fireEvent.change(searchField(), { target: { value: "pprd-2202" } });
    fireEvent.keyDown(searchField(), { key: "Enter" });

    expect(window.location.pathname).toBe("/clusters/pprd/nodes/pprd-2202");
  });

  it("goes nowhere on Enter when nothing answered", () => {
    useOverviewMock.mockReturnValue(state({ data: overview }));

    render(<App />);
    fireEvent.change(searchField(), { target: { value: "zzzz" } });
    fireEvent.keyDown(searchField(), { key: "Enter" });

    expect(window.location.pathname).toBe("/");
  });

  // Escape on a field that still holds a query undoes the query; on an empty
  // one it leaves the field. Two presses, two different things.
  it("clears the query on Escape before giving up the focus", () => {
    useOverviewMock.mockReturnValue(state({ data: overview }));

    render(<App />);
    const field = searchField();
    fireEvent.change(field, { target: { value: "pprd" } });
    expect(field).toHaveValue("pprd");

    fireEvent.keyDown(field, { key: "Escape" });
    expect(field).toHaveValue("");
    expect(within(screen.getByRole("tree")).getByText("Qualification")).toBeInTheDocument();
  });
});

describe("the alerts bell", () => {
  it("lists every alert of every cluster and leads to its cluster", () => {
    useOverviewMock.mockReturnValue(state({ data: overview }));

    render(<App />);
    fireEvent.click(screen.getByRole("button", { name: /Notifications/ }));

    const panel = screen.getByRole("menu", { name: "Alertes" });
    const items = within(panel).getAllByRole("menuitem");
    expect(items).toHaveLength(1);
    expect(items[0]).toHaveTextContent("Préproduction");

    const first = items[0];
    if (first === undefined) throw new Error("no alert in the panel");
    fireEvent.click(first);
    expect(window.location.pathname).toBe("/clusters/pprd");
  });

  it("says so when there is nothing to show", () => {
    useOverviewMock.mockReturnValue(
      state({
        data: {
          ...overview,
          clusters: overview.clusters.map((c) => ({ ...c, alerts: [] })),
        },
      }),
    );

    render(<App />);
    fireEvent.click(screen.getByRole("button", { name: "Notifications" }));

    expect(screen.getByText("Aucune alerte")).toBeInTheDocument();
  });
});

describe("what is polled, and what is not", () => {
  // A node or a guest view renders no card, and the hourly series of every
  // cluster was fetched all the same: N /rrd calls a minute, each an RRD read
  // upstream, for a chart nobody was looking at.
  it("asks for no cluster series while a detail screen is open", () => {
    useOverviewMock.mockReturnValue(state({ data: overview }));
    const fetchStub = vi.fn((_input: string | URL, _init?: RequestInit) => {
      return Promise.resolve(
        new Response(JSON.stringify({ points: [], cpuAverage: null }), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        }),
      );
    });
    vi.stubGlobal("fetch", fetchStub);
    window.history.replaceState(null, "", "/clusters/pprd/nodes/pprd-2201");

    render(<App />);

    // fetch is only ever called with a string URL here; the guard is for the
    // type, not for a case this test produces.
    const urls = fetchStub.mock.calls.map(([input]) =>
      typeof input === "string" ? input : input.toString(),
    );
    expect(urls.filter((url) => /\/api\/clusters\/[^/]+\/rrd/.test(url))).toEqual([]);
    vi.unstubAllGlobals();
  });

  // The overview's banner and the detail's used to fall stale together, so an
  // outage stacked two identical warnings on top of each other.
  it("shows one connection banner, not two", () => {
    useOverviewMock.mockReturnValue(
      state({ data: overview, isStale: true, error: new Error("boom") }),
    );
    window.history.replaceState(null, "", "/clusters/pprd/nodes/pprd-2201");

    render(<App />);

    expect(screen.queryAllByText(/connexion perdue/i)).toHaveLength(0);
  });

  it("still shows it on the overview itself", () => {
    useOverviewMock.mockReturnValue(
      state({ data: overview, isStale: true, error: new Error("boom") }),
    );

    render(<App />);

    expect(screen.getAllByText(/connexion perdue/i)).toHaveLength(1);
  });
});


/**
 * The display language, end to end.
 *
 * Everything below drives the real application — no component is rendered in
 * isolation — because the point of each case is that the CHOICE reaches every
 * screen at once, which is exactly what a component test cannot show.
 *
 * The suite runs in French (src/test/setup.ts pins the browser), so a test that
 * wants the other language says so itself.
 */
describe("App, in the browser's language", () => {
  beforeEach(() => {
    useOverviewMock.mockReturnValue(
      state({ data: overview, lastUpdatedAt: new Date("2026-09-12T14:32:00Z") }),
    );
  });

  it("comes up in French for a French browser, and says so on <html>", () => {
    render(<App />);

    expect(screen.getByRole("heading", { name: "Clusters" })).toBeInTheDocument();
    expect(screen.getByPlaceholderText("Rechercher une VM ou un nœud…")).toBeInTheDocument();
    expect(document.documentElement.getAttribute(LANG_ATTRIBUTE)).toBe("fr");
  });

  it("comes up in English for an English browser", () => {
    const restore = stubLanguages(["en-GB", "fr"]);
    try {
      render(<App />);

      expect(screen.getByPlaceholderText("Search for a VM or a node…")).toBeInTheDocument();
      expect(
        screen.getByRole("button", { name: "Language · Browser language" }),
      ).toBeInTheDocument();
      expect(document.documentElement.getAttribute(LANG_ATTRIBUTE)).toBe("en");
    } finally {
      restore();
    }
  });

  // A browser ranking German first, then English, wants English — not the
  // French sitting at the bottom of its list.
  it("reads the browser's ranking in order", () => {
    const restore = stubLanguages(["de", "en-US", "fr"]);
    try {
      render(<App />);

      expect(document.documentElement.getAttribute(LANG_ATTRIBUTE)).toBe("en");
    } finally {
      restore();
    }
  });

  it("falls back on French for a browser that asks for neither", () => {
    const restore = stubLanguages(["de", "es"]);
    try {
      render(<App />);

      expect(document.documentElement.getAttribute(LANG_ATTRIBUTE)).toBe("fr");
      expect(screen.getByRole("heading", { name: "Clusters" })).toBeInTheDocument();
    } finally {
      restore();
    }
  });

  it("switches the whole interface when the language is picked", () => {
    render(<App />);

    fireEvent.click(screen.getByRole("button", { name: /^Langue/ }));
    fireEvent.click(screen.getByRole("menuitemradio", { name: "English" }));

    // The top bar, a screen and the tree, which are three different components
    // and one language.
    expect(screen.getByPlaceholderText("Search for a VM or a node…")).toBeInTheDocument();
    expect(screen.getByRole("tree", { name: "Cluster tree" })).toBeInTheDocument();
    expect(document.documentElement.getAttribute(LANG_ATTRIBUTE)).toBe("en");
  });

  it("remembers the choice, the way the theme is remembered", () => {
    const { unmount } = render(<App />);

    fireEvent.click(screen.getByRole("button", { name: /^Langue/ }));
    fireEvent.click(screen.getByRole("menuitemradio", { name: "English" }));
    expect(window.localStorage.getItem(LANG_STORAGE_KEY)).toBe("en");

    unmount();
    render(<App />);

    expect(screen.getByPlaceholderText("Search for a VM or a node…")).toBeInTheDocument();
  });

  // The explicit choice is the point of having a control at all.
  it("keeps a chosen language against a browser that asks for the other", () => {
    window.localStorage.setItem(LANG_STORAGE_KEY, "fr");
    const restore = stubLanguages(["en-GB"]);
    try {
      render(<App />);

      expect(document.documentElement.getAttribute(LANG_ATTRIBUTE)).toBe("fr");
      expect(screen.getByPlaceholderText("Rechercher une VM ou un nœud…")).toBeInTheDocument();
    } finally {
      restore();
    }
  });

  it("goes back to following the browser, leaving nothing stored", () => {
    window.localStorage.setItem(LANG_STORAGE_KEY, "en");
    render(<App />);

    fireEvent.click(screen.getByRole("button", { name: /^Language/ }));
    fireEvent.click(screen.getByRole("menuitemradio", { name: "Browser language" }));

    expect(window.localStorage.getItem(LANG_STORAGE_KEY)).toBeNull();
    expect(document.documentElement.getAttribute(LANG_ATTRIBUTE)).toBe("fr");
  });

  // The figures are the other half of a language: the catalogue carries the
  // words, lib/format.ts carries the typography, and only the rendered screen
  // shows both arriving together.
  it("renders its figures in the typography of the language on screen", () => {
    const restore = stubLanguages(["en-GB"]);
    try {
      render(<App />);

      const main = within(screen.getByRole("main"));
      // 0.1589 of the memory, which French writes "16 %" with a narrow
      // no-break space and English writes flush against the sign.
      expect(main.getAllByText(/16%/).length).toBeGreaterThan(0);
      expect(main.queryByText(/16\u202f%/)).toBeNull();
    } finally {
      restore();
    }
  });
});
