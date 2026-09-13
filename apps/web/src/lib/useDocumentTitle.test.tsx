import { renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it } from "vitest";

import { useDocumentTitle } from "./useDocumentTitle";

beforeEach(() => {
  document.title = "";
});

afterEach(() => {
  document.title = "";
});

describe("useDocumentTitle", () => {
  // Ten tabs all called "moxy" are ten tabs nobody can tell apart, which is
  // exactly the state an incident leaves them in.
  it("writes the parts most specific first, closing on the product name", () => {
    renderHook(() => {
      useDocumentTitle("prox-pprd-2302-cit", "Préproduction");
    });

    expect(document.title).toBe("prox-pprd-2302-cit · Préproduction · moxy");
  });

  it("carries the product name alone when there is nothing else", () => {
    renderHook(() => {
      useDocumentTitle();
    });

    expect(document.title).toBe("moxy");
  });

  // A screen whose cluster name has not arrived yet renders a shorter title
  // rather than a "· ·".
  it.each([
    ["null", null],
    ["undefined", undefined],
    ["the empty string", ""],
    ["blank space", "   "],
  ])("drops a part that is %s", (_what, part) => {
    renderHook(() => {
      useDocumentTitle("Clusters", part);
    });

    expect(document.title).toBe("Clusters · moxy");
  });

  it("follows the parts when they change", () => {
    const { rerender } = renderHook(
      ({ name }: { name: string }) => {
        useDocumentTitle(name);
      },
      { initialProps: { name: "Clusters" } },
    );
    expect(document.title).toBe("Clusters · moxy");

    rerender({ name: "Préproduction" });

    expect(document.title).toBe("Préproduction · moxy");
  });
});
