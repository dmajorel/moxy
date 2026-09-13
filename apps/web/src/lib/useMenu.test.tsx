import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { useMenu } from "./useMenu";

/**
 * A menu reduced to what the hook drives: a button, a list, and nothing else.
 *
 * The two real menus test what they show — status dots, theme icons, French
 * labels. What is tested here is the behaviour they now share, on a component
 * small enough that a failure names the hook rather than a screen.
 */
function Menu({
  items,
  selectedIndex = 0,
  onActivate,
}: {
  items: string[];
  selectedIndex?: number;
  onActivate: (index: number) => void;
}) {
  const menu = useMenu({ count: items.length, selectedIndex, onActivate });
  return (
    // eslint-disable-next-line jsx-a11y/no-static-element-interactions -- event delegation, as in the real menus
    <div ref={menu.rootRef} onKeyDown={menu.onKeyDown}>
      <button
        ref={menu.buttonRef}
        type="button"
        aria-haspopup="menu"
        aria-expanded={menu.isOpen}
        onClick={menu.toggle}
      >
        Ouvrir
      </button>
      {menu.isOpen ? (
        <div role="menu" aria-label="Options">
          {items.map((item, index) => (
            <button
              key={item}
              ref={menu.itemRef(index)}
              type="button"
              role="menuitemradio"
              aria-checked={index === selectedIndex}
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

const ITEMS = ["un", "deux", "trois"];

function renderMenu(props: Partial<Parameters<typeof Menu>[0]> = {}) {
  const onActivate = vi.fn();
  const view = render(<Menu items={ITEMS} onActivate={onActivate} {...props} />);
  return { ...view, onActivate };
}

function button(): HTMLElement {
  return screen.getByRole("button", { name: "Ouvrir" });
}

function item(name: string): HTMLElement {
  return screen.getByRole("menuitemradio", { name });
}

function press(key: string) {
  fireEvent.keyDown(screen.getByRole("menu"), { key });
}

describe("useMenu", () => {
  it("opens and closes from the button, and says which it is", () => {
    renderMenu();

    expect(button()).toHaveAttribute("aria-expanded", "false");
    expect(screen.queryByRole("menu")).not.toBeInTheDocument();

    fireEvent.click(button());
    expect(button()).toHaveAttribute("aria-expanded", "true");
    expect(screen.getByRole("menu")).toBeInTheDocument();

    fireEvent.click(button());
    expect(screen.queryByRole("menu")).not.toBeInTheDocument();
  });

  // The focus moves for real rather than a highlight being painted on an item
  // the keyboard is not actually on.
  it("opens on the checked item and moves the focus with the arrows", () => {
    renderMenu({ selectedIndex: 1 });

    fireEvent.click(button());
    expect(item("deux")).toHaveFocus();

    press("ArrowDown");
    expect(item("trois")).toHaveFocus();

    // Past the end is the first item, not a dead end.
    press("ArrowDown");
    expect(item("un")).toHaveFocus();

    press("ArrowUp");
    expect(item("trois")).toHaveFocus();
  });

  it("opens on the first item when none is checked", () => {
    renderMenu({ selectedIndex: -1 });

    fireEvent.click(button());

    expect(item("un")).toHaveFocus();
  });

  it.each([["ArrowDown"], ["ArrowUp"]])("opens on %s from the button", (key) => {
    renderMenu();

    fireEvent.keyDown(button(), { key });

    expect(screen.getByRole("menu")).toBeInTheDocument();
    expect(item("un")).toHaveFocus();
  });

  it("jumps to the ends with Home and End", () => {
    renderMenu();

    fireEvent.click(button());
    press("End");
    expect(item("trois")).toHaveFocus();

    press("Home");
    expect(item("un")).toHaveFocus();
  });

  it.each([["Enter"], [" "]])("applies the active item on %s", (key) => {
    const { onActivate } = renderMenu();

    fireEvent.click(button());
    press("ArrowDown");
    press(key);

    expect(onActivate).toHaveBeenCalledTimes(1);
    expect(onActivate).toHaveBeenCalledWith(1);
    expect(screen.queryByRole("menu")).not.toBeInTheDocument();
    expect(button()).toHaveFocus();
  });

  it("applies an item on click and gives the focus back", () => {
    const { onActivate } = renderMenu();

    fireEvent.click(button());
    fireEvent.click(item("trois"));

    expect(onActivate).toHaveBeenCalledWith(2);
    expect(screen.queryByRole("menu")).not.toBeInTheDocument();
    expect(button()).toHaveFocus();
  });

  it("closes on Escape and hands the focus back to the button", () => {
    renderMenu();

    fireEvent.click(button());
    press("Escape");

    expect(screen.queryByRole("menu")).not.toBeInTheDocument();
    expect(button()).toHaveFocus();
  });

  // Tab is a move, not a cancellation: the focus goes on to whatever comes
  // next in the page rather than being dragged back to the button.
  it("closes on Tab without taking the focus back", () => {
    renderMenu();

    fireEvent.click(button());
    press("Tab");

    expect(screen.queryByRole("menu")).not.toBeInTheDocument();
    expect(button()).not.toHaveFocus();
  });

  it("ignores a key it does not handle", () => {
    renderMenu();

    fireEvent.keyDown(button(), { key: "a" });
    expect(screen.queryByRole("menu")).not.toBeInTheDocument();

    fireEvent.click(button());
    press("a");
    expect(screen.getByRole("menu")).toBeInTheDocument();
  });

  // A menu that outlives a click elsewhere is a trap. The focus is only taken
  // back when it was still inside the menu, so a click aimed at something else
  // lands where it was aimed.
  it("closes on a click outside and leaves an outside focus alone", () => {
    renderMenu();
    const elsewhere = document.createElement("button");
    document.body.append(elsewhere);

    fireEvent.click(button());
    elsewhere.focus();
    fireEvent.mouseDown(elsewhere);

    expect(screen.queryByRole("menu")).not.toBeInTheDocument();
    expect(button()).not.toHaveFocus();
    elsewhere.remove();
  });

  it("gives the focus back when the outside click lands on nothing focusable", () => {
    renderMenu();

    fireEvent.click(button());
    expect(item("un")).toHaveFocus();
    fireEvent.mouseDown(document.body);

    expect(screen.queryByRole("menu")).not.toBeInTheDocument();
    expect(button()).toHaveFocus();
  });

  it("stays open on a click inside it", () => {
    renderMenu();

    fireEvent.click(button());
    fireEvent.mouseDown(screen.getByRole("menu"));

    expect(screen.getByRole("menu")).toBeInTheDocument();
  });

  // A menu of nothing is not a crash: the index arithmetic stays total.
  it("survives an empty list", () => {
    const { onActivate } = renderMenu({ items: [], selectedIndex: -1 });

    fireEvent.click(button());
    press("ArrowDown");
    press("End");
    press("Enter");

    expect(onActivate).toHaveBeenCalledWith(0);
  });
});
