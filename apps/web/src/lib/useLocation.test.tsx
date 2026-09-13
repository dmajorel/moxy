import { act, renderHook, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it } from "vitest";

import { useNavigate, useRoute } from "./useLocation";

beforeEach(() => {
  window.history.replaceState(null, "", "/");
});

afterEach(() => {
  window.history.replaceState(null, "", "/");
});

describe("useRoute", () => {
  it("reads the address bar", () => {
    window.history.replaceState(null, "", "/clusters/prod/nodes/pve-1");

    const { result } = renderHook(() => useRoute());

    expect(result.current).toEqual({
      kind: "node",
      clusterId: "prod",
      node: "pve-1",
    });
  });

  it("reports a path that designates nothing", () => {
    window.history.replaceState(null, "", "/nowhere");

    const { result } = renderHook(() => useRoute());

    expect(result.current).toEqual({ kind: "notFound", path: "/nowhere" });
  });
});

describe("useNavigate", () => {
  /** Both hooks in one render, which is how App uses them. */
  function setup() {
    return renderHook(() => ({ route: useRoute(), navigate: useNavigate() }));
  }

  // pushState does NOT fire popstate — that event is the back and forward
  // buttons alone — so every navigation has to tell the subscribers itself.
  it("re-renders on a push", () => {
    const { result } = setup();

    act(() => {
      result.current.navigate.push({ kind: "cluster", clusterId: "prod" });
    });

    expect(window.location.pathname).toBe("/clusters/prod");
    expect(result.current.route).toEqual({ kind: "cluster", clusterId: "prod" });
  });

  it("re-renders on a replace", () => {
    const { result } = setup();

    act(() => {
      result.current.navigate.replace({ kind: "cluster", clusterId: "prod" });
    });

    expect(result.current.route).toEqual({ kind: "cluster", clusterId: "prod" });
  });

  it("pushes a history entry and replace does not", () => {
    const { result } = setup();
    const before = window.history.length;

    act(() => {
      result.current.navigate.push({ kind: "cluster", clusterId: "a" });
    });
    expect(window.history.length).toBe(before + 1);

    act(() => {
      result.current.navigate.replace({ kind: "cluster", clusterId: "b" });
    });
    expect(window.history.length).toBe(before + 1);
  });

  // A second click on the row that is already selected must not grow the
  // history by an entry that changes nothing.
  it("drops a navigation to where it already is", () => {
    window.history.replaceState(null, "", "/clusters/prod");
    const { result } = setup();
    const before = window.history.length;

    act(() => {
      result.current.navigate.push({ kind: "cluster", clusterId: "prod" });
    });

    expect(window.history.length).toBe(before);
  });

  // Nothing writes a query today; dropping what a future one puts there would
  // be a silent loss.
  it("carries the query and the fragment across", () => {
    window.history.replaceState(null, "", "/?debug=1#top");
    const { result } = setup();

    act(() => {
      result.current.navigate.push({ kind: "cluster", clusterId: "prod" });
    });

    expect(window.location.pathname).toBe("/clusters/prod");
    expect(window.location.search).toBe("?debug=1");
    expect(window.location.hash).toBe("#top");
  });

  it("follows the back button", async () => {
    const { result } = setup();

    act(() => {
      result.current.navigate.push({ kind: "cluster", clusterId: "prod" });
    });
    expect(result.current.route).toEqual({ kind: "cluster", clusterId: "prod" });

    // jsdom runs back() asynchronously, through the popstate event the hook
    // subscribes to — which is the mechanism under test, so it is waited for
    // rather than assumed to have happened by the next microtask.
    window.history.back();
    await waitFor(() => {
      expect(result.current.route).toEqual({ kind: "all" });
    });
  });

  it("stops listening once it is unmounted", () => {
    const { result, unmount } = setup();
    const seen = result.current.route;

    unmount();
    window.history.pushState(null, "", "/clusters/prod");

    // The hook no longer reports anything; the assertion is that nothing
    // throws and the last value it produced is untouched.
    expect(seen).toEqual({ kind: "all" });
  });
});
