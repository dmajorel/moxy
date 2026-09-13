import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { useMenu } from "./useMenu";

/**
 * The machine itself, away from any of the three controls that use it.
 *
 * The switcher, the theme toggle and the bell each keep their own test of what
 * they show; what is checked here is the behaviour they share, which used to be
 * asserted three times over three copies that had already drifted apart.
 */

interface HarnessProps {
  items?: string[];
  onActivate?: (index: number) => void;
  initialIndex?: () => number;
}

function Harness({ items = ["a", "b", "c"], onActivate, initialIndex }: HarnessProps) {
  const menu = useMenu({
    count: items.length,
    initialIndex,
    onActivate: onActivate ?? (() => undefined),
  });

  return (
    // eslint-disable-next-line jsx-a11y/no-static-element-interactions -- event delegation, as in the real controls
    <div ref={menu.rootRef} onKeyDown={menu.onKeyDown}>
      <button {...menu.buttonProps} type="button">
        Ouvrir
      </button>
      {menu.isOpen ? (
        <div role="menu" aria-label="Test">
          {items.map((item, index) => (
            <button
              key={item}
              ref={menu.itemRef(index)}
              type="button"
              role="menuitem"
              tabIndex={-1}
              onClick={() => {
                menu.activate(index);
              }}
            >
              {item}
            </button>
          ))}
        </div>
      ) : null}
    </div>
  );
}

function setup(props: HarnessProps = {}) {
  render(
    <>
      <Harness {...props} />
      <button type="button">Ailleurs</button>
    </>,
  );
  const trigger = screen.getByRole("button", { name: "Ouvrir" });
  return { trigger };
}

function open(props: HarnessProps = {}) {
  const { trigger } = setup(props);
  fireEvent.click(trigger);
  return { trigger };
}

describe("useMenu", () => {
  it("starts closed and says so on the trigger", () => {
    const { trigger } = setup();

    expect(screen.queryByRole("menu")).toBeNull();
    expect(trigger).toHaveAttribute("aria-expanded", "false");
    expect(trigger).toHaveAttribute("aria-haspopup", "menu");
  });

  it("opens on a click and closes on the next one", () => {
    const { trigger } = setup();

    fireEvent.click(trigger);
    expect(screen.getByRole("menu")).toBeInTheDocument();
    expect(trigger).toHaveAttribute("aria-expanded", "true");

    fireEvent.click(trigger);
    expect(screen.queryByRole("menu")).toBeNull();
  });

  // Arrow keys move the focus for real, rather than only painting a highlight:
  // a screen reader follows the focus and nothing else.
  it("opens from the keyboard, on the first item", () => {
    const { trigger } = setup();

    fireEvent.keyDown(trigger, { key: "ArrowDown" });

    expect(document.activeElement).toBe(screen.getAllByRole("menuitem")[0]);
  });

  it("opens on ArrowUp as well", () => {
    const { trigger } = setup();

    fireEvent.keyDown(trigger, { key: "ArrowUp" });

    expect(screen.getByRole("menu")).toBeInTheDocument();
  });

  it("opens where the caller says, not always on the first item", () => {
    open({ initialIndex: () => 2 });

    expect(document.activeElement).toBe(screen.getAllByRole("menuitem")[2]);
  });

  // findIndex answers -1 for something that is not in the list, which would
  // otherwise focus nothing at all.
  it("falls back to the first item when the caller finds none", () => {
    open({ initialIndex: () => -1 });

    expect(document.activeElement).toBe(screen.getAllByRole("menuitem")[0]);
  });

  it("wraps the arrows around both ends", () => {
    open();
    const items = screen.getAllByRole("menuitem");
    const menu = screen.getByRole("menu");

    fireEvent.keyDown(menu, { key: "ArrowUp" });
    expect(document.activeElement).toBe(items[2]);

    fireEvent.keyDown(menu, { key: "ArrowDown" });
    expect(document.activeElement).toBe(items[0]);
  });

  it("jumps to the ends with Home and End", () => {
    open();
    const items = screen.getAllByRole("menuitem");
    const menu = screen.getByRole("menu");

    fireEvent.keyDown(menu, { key: "End" });
    expect(document.activeElement).toBe(items[2]);

    fireEvent.keyDown(menu, { key: "Home" });
    expect(document.activeElement).toBe(items[0]);
  });

  it("activates the focused item on Enter, and closes", () => {
    const onActivate = vi.fn();
    open({ onActivate });
    fireEvent.keyDown(screen.getByRole("menu"), { key: "ArrowDown" });

    fireEvent.keyDown(screen.getByRole("menu"), { key: "Enter" });

    expect(onActivate).toHaveBeenCalledWith(1);
    expect(screen.queryByRole("menu")).toBeNull();
  });

  it("activates on Space too", () => {
    const onActivate = vi.fn();
    open({ onActivate });

    fireEvent.keyDown(screen.getByRole("menu"), { key: " " });

    expect(onActivate).toHaveBeenCalledWith(0);
  });

  it("activates on a click of an item", () => {
    const onActivate = vi.fn();
    open({ onActivate });

    fireEvent.click(screen.getAllByRole("menuitem")[1] as HTMLElement);

    expect(onActivate).toHaveBeenCalledWith(1);
    expect(screen.queryByRole("menu")).toBeNull();
  });

  // Every menu of this bar closes on Escape and hands the focus back: a popover
  // that keeps it is a trap.
  it("closes on Escape and returns the focus to the trigger", () => {
    const { trigger } = open();

    fireEvent.keyDown(screen.getByRole("menu"), { key: "Escape" });

    expect(screen.queryByRole("menu")).toBeNull();
    expect(document.activeElement).toBe(trigger);
  });

  // Tab is a deliberate move onwards: dragging the focus back would undo it.
  it("closes on Tab without stealing the focus back", () => {
    const { trigger } = open();

    fireEvent.keyDown(screen.getByRole("menu"), { key: "Tab" });

    expect(screen.queryByRole("menu")).toBeNull();
    expect(document.activeElement).not.toBe(trigger);
  });

  it("ignores a key it does not handle", () => {
    open();

    fireEvent.keyDown(screen.getByRole("menu"), { key: "x" });

    expect(screen.getByRole("menu")).toBeInTheDocument();
  });

  it("closes when a click lands outside it", () => {
    open();

    fireEvent.mouseDown(document.body);

    expect(screen.queryByRole("menu")).toBeNull();
  });

  it("stays open for a click inside it", () => {
    open();

    fireEvent.mouseDown(screen.getByRole("menu"));

    expect(screen.getByRole("menu")).toBeInTheDocument();
  });

  // The focus goes back to the trigger only when it was still inside: a click
  // aimed at another control must land where the user aimed it.
  it("leaves the focus alone when the outside click took it elsewhere", () => {
    open();
    const elsewhere = screen.getByRole("button", { name: "Ailleurs" });
    elsewhere.focus();

    fireEvent.mouseDown(elsewhere);

    expect(document.activeElement).toBe(elsewhere);
  });

  // The bell opens on an estate with nothing wrong, and used to walk its arrows
  // over an empty list.
  describe("an empty menu", () => {
    it("still opens, and focuses nothing", () => {
      open({ items: [] });

      expect(screen.getByRole("menu")).toBeInTheDocument();
      expect(screen.queryByRole("menuitem")).toBeNull();
    });

    it("survives the arrows, Home and End", () => {
      open({ items: [] });
      const menu = screen.getByRole("menu");

      for (const key of ["ArrowDown", "ArrowUp", "Home", "End"]) {
        fireEvent.keyDown(menu, { key });
      }

      expect(screen.getByRole("menu")).toBeInTheDocument();
    });

    it("activates nothing on Enter", () => {
      const onActivate = vi.fn();
      open({ items: [], onActivate });

      fireEvent.keyDown(screen.getByRole("menu"), { key: "Enter" });

      expect(onActivate).not.toHaveBeenCalled();
    });
  });
});
