import { useCallback, useEffect, useRef, useState } from "react";
import type { KeyboardEvent, RefObject } from "react";

/**
 * The behaviour of a popup menu: open state, roving focus, dismissal.
 *
 * The cluster switcher and the theme toggle held one copy each of the same
 * hundred lines — Escape, the arrows, Home/End, Tab, the outside click, and the
 * focus that must land on the active item for real rather than be painted on
 * it. Two copies means an accessibility fix applied once and forgotten once,
 * and the difference is invisible until somebody drives the second menu with a
 * keyboard.
 *
 * What stays with the caller is everything visible: the list of options, their
 * labels, their icons, what a choice does. The hook knows only how many items
 * there are and which one is checked.
 */
export interface UseMenuOptions {
  /** How many items the menu lists. */
  count: number;
  /**
   * Index of the checked item, which is where the menu opens. `-1` when none
   * is checked, and the menu then opens on the first item.
   */
  selectedIndex: number;
  /**
   * Applies the choice at `index`. The menu closes and hands the focus back to
   * its button on its own, so this only has to do the choosing.
   */
  onActivate: (index: number) => void;
}

export interface Menu {
  isOpen: boolean;
  /** The item the keyboard is on. Meaningful only while open. */
  activeIndex: number;
  /** Goes on the wrapper, which also carries `onKeyDown`. */
  rootRef: RefObject<HTMLDivElement | null>;
  /** Goes on the button that opens the menu. */
  buttonRef: RefObject<HTMLButtonElement | null>;
  /** Goes on item `index`, so the arrows can move the focus onto it. */
  itemRef: (index: number) => (element: HTMLButtonElement | null) => void;
  /**
   * Goes on the wrapper: it routes the keys of the button and of the items it
   * holds, by delegation, and is itself neither focusable nor clickable.
   */
  onKeyDown: (event: KeyboardEvent<HTMLElement>) => void;
  /** The button's `onClick`. */
  toggle: () => void;
  /** An item's `onClick`. */
  activate: (index: number) => void;
}

export function useMenu({
  count,
  selectedIndex,
  onActivate,
}: UseMenuOptions): Menu {
  const [isOpen, setIsOpen] = useState(false);
  const [activeIndex, setActiveIndex] = useState(0);
  const rootRef = useRef<HTMLDivElement>(null);
  const buttonRef = useRef<HTMLButtonElement>(null);
  const itemRefs = useRef<Array<HTMLButtonElement | null>>([]);

  // Read through a ref rather than closed over: the caller rebuilds its list
  // and its callback on every render, and depending on them would re-create
  // every handler below — and with them the outside-click listener — each time.
  const latest = useRef({ count, selectedIndex, onActivate });
  latest.current = { count, selectedIndex, onActivate };

  const close = useCallback((restoreFocus: boolean) => {
    setIsOpen(false);
    if (restoreFocus) {
      buttonRef.current?.focus();
    }
  }, []);

  const open = useCallback(() => {
    const index = latest.current.selectedIndex;
    setActiveIndex(index < 0 ? 0 : index);
    setIsOpen(true);
  }, []);

  const activate = useCallback(
    (index: number) => {
      latest.current.onActivate(index);
      close(true);
    },
    [close],
  );

  const toggle = useCallback(() => {
    if (isOpen) {
      close(false);
    } else {
      open();
    }
  }, [isOpen, close, open]);

  // A menu that outlives a click elsewhere is a trap; focus only goes back to
  // the button when it was still inside the menu, so an outside click is free
  // to land wherever the user aimed.
  useEffect(() => {
    if (!isOpen) {
      return;
    }
    function onMouseDown(event: MouseEvent) {
      const root = rootRef.current;
      if (root === null || root.contains(event.target as Node)) {
        return;
      }
      setIsOpen(false);
      if (root.contains(document.activeElement)) {
        buttonRef.current?.focus();
      }
    }
    document.addEventListener("mousedown", onMouseDown);
    return () => {
      document.removeEventListener("mousedown", onMouseDown);
    };
  }, [isOpen]);

  // Arrow keys move focus for real, rather than only painting a highlight.
  useEffect(() => {
    if (isOpen) {
      itemRefs.current[activeIndex]?.focus();
    }
  }, [isOpen, activeIndex]);

  const itemRef = useCallback(
    (index: number) => (element: HTMLButtonElement | null) => {
      itemRefs.current[index] = element;
    },
    [],
  );

  const onKeyDown = useCallback(
    (event: KeyboardEvent<HTMLElement>) => {
      const { count: length } = latest.current;

      if (!isOpen) {
        if (event.key === "ArrowDown" || event.key === "ArrowUp") {
          event.preventDefault();
          open();
        }
        return;
      }

      switch (event.key) {
        case "Escape":
          event.preventDefault();
          close(true);
          break;
        case "ArrowDown":
          event.preventDefault();
          setActiveIndex((index) => wrap(index + 1, length));
          break;
        case "ArrowUp":
          event.preventDefault();
          setActiveIndex((index) => wrap(index - 1, length));
          break;
        case "Home":
          event.preventDefault();
          setActiveIndex(0);
          break;
        case "End":
          event.preventDefault();
          setActiveIndex(Math.max(0, length - 1));
          break;
        case "Enter":
        case " ":
          // preventDefault also cancels the browser's own activation of the
          // focused button, so the choice is not applied twice.
          event.preventDefault();
          activate(activeIndex);
          break;
        case "Tab":
          close(false);
          break;
        default:
          break;
      }
    },
    [isOpen, activeIndex, open, close, activate],
  );

  return {
    isOpen,
    activeIndex,
    rootRef,
    buttonRef,
    itemRef,
    onKeyDown,
    toggle,
    activate,
  };
}

/** Wraps an index around a list, and stays total on an empty one. */
function wrap(index: number, count: number): number {
  if (count <= 0) {
    return 0;
  }
  return ((index % count) + count) % count;
}
