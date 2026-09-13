import { fireEvent, render, screen } from "@testing-library/react";
import { useRef } from "react";
import { describe, expect, it } from "vitest";

import { useFocusTrap } from "./useFocusTrap";

function Trapped({ children }: { children?: React.ReactNode }) {
  const ref = useRef<HTMLDivElement>(null);
  useFocusTrap(ref);
  return (
    <div ref={ref} data-testid="trap">
      {children ?? (
        <>
          <button type="button">premier</button>
          <button type="button">milieu</button>
          <button type="button">dernier</button>
        </>
      )}
    </div>
  );
}

/** A control outside the trap, standing for the tree or the top bar under it. */
function background(): HTMLButtonElement {
  const button = document.createElement("button");
  button.textContent = "arrière-plan";
  document.body.append(button);
  return button;
}

describe("useFocusTrap", () => {
  it("focuses the first control on open", () => {
    render(<Trapped />);
    expect(document.activeElement).toBe(screen.getByRole("button", { name: "premier" }));
  });

  // Tab from the last control used to walk out into the page underneath, which
  // is a modal that is not modal.
  it("wraps forward from the last control to the first", () => {
    render(<Trapped />);
    screen.getByRole("button", { name: "dernier" }).focus();

    fireEvent.keyDown(document, { key: "Tab" });

    expect(document.activeElement).toBe(screen.getByRole("button", { name: "premier" }));
  });

  it("wraps backward from the first to the last", () => {
    render(<Trapped />);
    screen.getByRole("button", { name: "premier" }).focus();

    fireEvent.keyDown(document, { key: "Tab", shiftKey: true });

    expect(document.activeElement).toBe(screen.getByRole("button", { name: "dernier" }));
  });

  // In the middle of the list the browser's own behaviour is correct and must
  // not be second-guessed: intercepting every Tab would break the order of the
  // controls it is supposed to preserve.
  it("leaves the browser alone in the middle", () => {
    render(<Trapped />);
    const middle = screen.getByRole("button", { name: "milieu" });
    middle.focus();

    fireEvent.keyDown(document, { key: "Tab" });

    expect(document.activeElement).toBe(middle);
  });

  it("pulls the focus back in when it is outside", () => {
    const outside = background();
    render(<Trapped />);
    outside.focus();

    fireEvent.keyDown(document, { key: "Tab" });

    expect(document.activeElement).toBe(screen.getByRole("button", { name: "premier" }));
    outside.remove();
  });

  // Closing dropped the focus on <body>, returning a keyboard user to the top
  // of the document rather than to the control they had pressed.
  it("gives the focus back to whatever opened it", () => {
    const trigger = background();
    trigger.focus();

    const view = render(<Trapped />);
    expect(document.activeElement).not.toBe(trigger);

    view.unmount();

    expect(document.activeElement).toBe(trigger);
    trigger.remove();
  });

  // A disabled control is not a tab stop, and cycling onto one would look like
  // the focus vanishing.
  it("skips what the keyboard cannot reach", () => {
    render(
      <Trapped>
        <button type="button">premier</button>
        <button type="button" disabled>
          désactivé
        </button>
        <button type="button" tabIndex={-1}>
          hors tabulation
        </button>
        <button type="button">dernier</button>
      </Trapped>,
    );

    screen.getByRole("button", { name: "dernier" }).focus();
    fireEvent.keyDown(document, { key: "Tab" });
    expect(document.activeElement).toBe(screen.getByRole("button", { name: "premier" }));

    fireEvent.keyDown(document, { key: "Tab", shiftKey: true });
    expect(document.activeElement).toBe(screen.getByRole("button", { name: "dernier" }));
  });

  // Nothing to land on: the keyboard stays where it is rather than walking
  // into a background the operator cannot see.
  it("holds the keyboard when there is no control at all", () => {
    const outside = background();
    render(
      <Trapped>
        <p>rien à focaliser</p>
      </Trapped>,
    );
    outside.focus();

    fireEvent.keyDown(document, { key: "Tab" });

    expect(document.activeElement).toBe(outside);
    outside.remove();
  });

  it("ignores every key but Tab", () => {
    render(<Trapped />);
    const middle = screen.getByRole("button", { name: "milieu" });
    middle.focus();

    fireEvent.keyDown(document, { key: "ArrowDown" });

    expect(document.activeElement).toBe(middle);
  });
});
