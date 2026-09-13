import { fireEvent, render, screen } from "@testing-library/react";
import { useState } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { ErrorBoundary } from "./ErrorBoundary";

/** A component that throws on demand, which is what a broken payload does. */
function Bomb({ armed }: { armed: boolean }) {
  if (armed) {
    throw new TypeError("Cannot read properties of undefined (reading 'ratio')");
  }
  return <p>contenu</p>;
}

beforeEach(() => {
  // React logs the caught error itself, on top of what the boundary logs. The
  // test output is not the place to read it.
  vi.spyOn(console, "error").mockImplementation(() => undefined);
});

afterEach(() => {
  vi.restoreAllMocks();
});

describe("ErrorBoundary", () => {
  it("renders its children while nothing throws", () => {
    render(
      <ErrorBoundary>
        <Bomb armed={false} />
      </ErrorBoundary>,
    );

    expect(screen.getByText("contenu")).toBeInTheDocument();
  });

  // Without this, React unmounts the entire tree: one missing field in one
  // payload used to take the top bar and the tree down with the screen.
  it("catches a render failure and explains it", () => {
    render(
      <ErrorBoundary>
        <Bomb armed />
      </ErrorBoundary>,
    );

    expect(screen.getByRole("alert")).toBeInTheDocument();
    expect(screen.getByText("Impossible de charger les données")).toBeInTheDocument();
    // The exception text is diagnostic material, not a label: ErrorView keeps
    // it behind the collapsed "Détail technique" disclosure.
    const technical = screen.getByText(/Cannot read properties/);
    expect(technical.closest("details")).not.toBeNull();
  });

  it("logs what broke, with the component stack", () => {
    render(
      <ErrorBoundary label="the node view">
        <Bomb armed />
      </ErrorBoundary>,
    );

    const logged = vi.mocked(console.error).mock.calls.flat().join(" ");
    expect(logged).toContain("the node view");
    expect(logged).toContain("Cannot read properties");
  });

  // Clearing the error alone would re-render the very components that threw,
  // with their state intact, and they would throw again. The retry remounts.
  it("remounts the subtree on a retry", () => {
    function Wrapper() {
      const [armed, setArmed] = useState(true);
      return (
        <>
          <button
            type="button"
            onClick={() => {
              setArmed(false);
            }}
          >
            désamorcer
          </button>
          <ErrorBoundary>
            <Bomb armed={armed} />
          </ErrorBoundary>
        </>
      );
    }

    render(<Wrapper />);
    expect(screen.getByRole("alert")).toBeInTheDocument();

    // The cause is fixed outside the boundary — a payload that arrives whole
    // on the next poll — and only then does the retry have anything to mount.
    fireEvent.click(screen.getByRole("button", { name: "désamorcer" }));
    fireEvent.click(screen.getByRole("button", { name: "Réessayer" }));

    expect(screen.getByText("contenu")).toBeInTheDocument();
    expect(screen.queryByRole("alert")).toBeNull();
  });

  // A wrapper element here would change the layout of everything the boundary
  // guards: the content pane and the detail screens lay their children out
  // directly.
  it("adds no element of its own", () => {
    const { container } = render(
      <ErrorBoundary>
        <p>contenu</p>
      </ErrorBoundary>,
    );

    expect(container.firstElementChild?.tagName).toBe("P");
  });

  // Anything may be thrown in JavaScript, and a string thrown by a dependency
  // must not become a second failure inside the boundary.
  it("survives something that is not an Error", () => {
    function ThrowsAString(): never {
      // eslint-disable-next-line @typescript-eslint/only-throw-error -- the point.
      throw "boom";
    }

    render(
      <ErrorBoundary>
        <ThrowsAString />
      </ErrorBoundary>,
    );

    expect(screen.getByRole("alert")).toBeInTheDocument();
  });
});
