/**
 * The one menu state machine of the top bar.
 *
 * Three controls opened a list of options — the cluster switcher, the theme
 * toggle and the notification bell — and each carried its own copy of the same
 * hundred lines: open and close, the active index, Escape, the arrows, Home and
 * End, Tab, the outside click, and the effect that moves the real focus onto the
 * active item. Three copies meant that every accessibility fix had to be made
 * three times, and the third copy had already drifted: its Home and End guarded
 * against an empty list, the other two did not.
 *
 * What stays with each caller is what actually differs: what the items are, what
 * they look like, and what activating one does.
 */
import { useCallback, useEffect, useRef, useState } from "react";
import type { KeyboardEvent, RefObject } from "react";

export interface MenuOptions {
  /** How many items the menu holds. Zero is legitimate: an empty list. */
  count: number;
  /**
   * Which item the menu opens on.
   *
   * Default: the first. The switcher and the theme toggle pass the index of the
   * current choice instead, so opening a menu lands on what is selected rather
   * than making the operator walk to it.
   */
  initialIndex?: () => number;
  /** Enter, Space or a click on the item at `index`. The menu then closes. */
  onActivate: (index: number) => void;
}

export interface Menu {
  isOpen: boolean;
  /** Which item has the focus; meaningless while closed. */
  activeIndex: number;
  /** On the wrapper: the outside-click detection needs its bounds. */
  rootRef: RefObject<HTMLDivElement | null>;
  /**
   * On the wrapper, which routes the keys of the button and of the items it
   * holds. The wrapper itself is neither focusable nor clickable.
   */
  onKeyDown: (event: KeyboardEvent<HTMLElement>) => void;
  /** Spread onto the trigger. */
  buttonProps: {
    ref: RefObject<HTMLButtonElement | null>;
    "aria-haspopup": "menu";
    "aria-expanded": boolean;
    onClick: () => void;
  };
  /** The ref of item `index`, so the arrows move the focus for real. */
  itemRef: (index: number) => (element: HTMLButtonElement | null) => void;
  /** What a click on item `index` does: activate it, then close. */
  activate: (index: number) => void;
  /**
   * Closes the menu.
   *
   * `restoreFocus` is false for a Tab or a click outside, which are the two
   * ways of leaving on purpose: dragging the focus back would undo the move the
   * user just made.
   */
  close: (restoreFocus: boolean) => void;
}

export function useMenu({ count, initialIndex, onActivate }: MenuOptions): Menu {
  const [isOpen, setIsOpen] = useState(false);
  const [activeIndex, setActiveIndex] = useState(0);
  const rootRef = useRef<HTMLDivElement>(null);
  const buttonRef = useRef<HTMLButtonElement>(null);
  const itemRefs = useRef<(HTMLButtonElement | null)[]>([]);

  /** Read from a handler that must not re-subscribe when the list changes. */
  const initialRef = useRef(initialIndex);
  initialRef.current = initialIndex;

  const close = useCallback((restoreFocus: boolean) => {
    setIsOpen(false);
    if (restoreFocus) {
      buttonRef.current?.focus();
    }
  }, []);

  const open = useCallback(() => {
    const index = initialRef.current?.() ?? 0;
    setActiveIndex(index < 0 ? 0 : index);
    setIsOpen(true);
  }, []);

  const activate = useCallback(
    (index: number) => {
      onActivate(index);
      close(true);
    },
    [close, onActivate],
  );

  // A menu that outlives a click elsewhere is a trap; the focus only goes back
  // to the button when it was still inside, so an outside click is free to land
  // wherever the user aimed it.
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

  // Arrow keys move the focus for real, rather than only painting a highlight.
  useEffect(() => {
    if (isOpen && count > 0) {
      itemRefs.current[activeIndex]?.focus();
    }
  }, [isOpen, activeIndex, count]);

  const onKeyDown = useCallback(
    (event: KeyboardEvent<HTMLElement>) => {
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
          if (count > 0) {
            setActiveIndex((index) => (index + 1) % count);
          }
          break;
        case "ArrowUp":
          event.preventDefault();
          if (count > 0) {
            setActiveIndex((index) => (index - 1 + count) % count);
          }
          break;
        case "Home":
          event.preventDefault();
          setActiveIndex(0);
          break;
        case "End":
          event.preventDefault();
          setActiveIndex(Math.max(0, count - 1));
          break;
        case "Enter":
        case " ":
          // preventDefault also cancels the browser's own activation of the
          // focused button, so the choice is not applied twice.
          event.preventDefault();
          if (count > 0) {
            activate(activeIndex);
          }
          break;
        case "Tab":
          close(false);
          break;
        default:
          break;
      }
    },
    [activate, activeIndex, close, count, isOpen, open],
  );

  const itemRef = useCallback(
    (index: number) => (element: HTMLButtonElement | null) => {
      itemRefs.current[index] = element;
    },
    [],
  );

  return {
    isOpen,
    activeIndex,
    rootRef,
    onKeyDown,
    buttonProps: {
      ref: buttonRef,
      "aria-haspopup": "menu",
      "aria-expanded": isOpen,
      onClick: () => {
        if (isOpen) {
          close(false);
        } else {
          open();
        }
      },
    },
    itemRef,
    activate,
    close,
  };
}
