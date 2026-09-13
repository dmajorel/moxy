import { useEffect, useRef } from "react";
import type { RefObject } from "react";

/**
 * Keeps the keyboard inside an open dialog, and gives it back on close.
 *
 * Both halves were missing. Tab from the close button walked out of the
 * maintenance dialog into the tree and the top bar underneath, which is a
 * modal that is not modal; and closing it dropped focus on <body>, so the
 * keyboard user was returned to the top of the document rather than to the
 * button they had pressed. CLAUDE.md already requires the second of every
 * menu: "se ferment à Échap en rendant le focus".
 *
 * It is written by hand rather than delegated to <dialog>.showModal() for one
 * reason: showModal moves the element into the browser's top layer, which
 * changes how the overlay stacks and how the tests reach it, for behaviour
 * that is thirty lines here.
 */

/**
 * What counts as reachable by Tab.
 *
 * `[tabindex="-1"]` is excluded on purpose: it is focusable by script and not
 * by the keyboard, which is exactly the distinction a trap has to make.
 */
const FOCUSABLE = [
  "a[href]",
  "button:not([disabled])",
  "input:not([disabled])",
  "select:not([disabled])",
  "textarea:not([disabled])",
  '[tabindex]:not([tabindex="-1"])',
].join(",");

function focusableIn(container: HTMLElement): HTMLElement[] {
  return Array.from(container.querySelectorAll<HTMLElement>(FOCUSABLE)).filter(
    // A control inside a [hidden] subtree is in the DOM and not on the screen;
    // cycling onto it would look like the focus vanishing.
    //
    // The check is on the attribute rather than on offsetParent, which is the
    // usual idiom: jsdom performs no layout, so offsetParent is null for every
    // element and the usual idiom filters out the whole dialog under test.
    (element) => element.closest("[hidden]") === null,
  );
}

/**
 * Traps the keyboard inside `container` while it is mounted, and restores the
 * focus to whatever held it before.
 *
 * The trigger is not passed in: the element that had focus when the dialog
 * opened IS the trigger, whichever it was, and remembering it here means a
 * caller cannot forget to.
 */
export function useFocusTrap(container: RefObject<HTMLElement | null>): void {
  const returnTo = useRef<HTMLElement | null>(null);

  useEffect(() => {
    returnTo.current =
      document.activeElement instanceof HTMLElement ? document.activeElement : null;

    const root = container.current;
    // Focus lands on the first control rather than on the dialog itself: the
    // close button is the way out, and putting the caret there means Escape
    // and Enter both do something from the first keystroke.
    const first = root === null ? undefined : focusableIn(root)[0];
    first?.focus();

    return () => {
      // The element may have been removed while the dialog was open — the node
      // it belonged to disappeared from the overview, say. Focusing a detached
      // element silently does nothing, which is the right failure.
      returnTo.current?.focus();
    };
  }, [container]);

  useEffect(() => {
    function onKeyDown(event: KeyboardEvent) {
      if (event.key !== "Tab") {
        return;
      }
      const root = container.current;
      if (root === null) {
        return;
      }
      const focusable = focusableIn(root);
      const first = focusable[0];
      const last = focusable[focusable.length - 1];
      if (first === undefined || last === undefined) {
        // Nothing to land on: keep the keyboard where it is rather than let it
        // walk into a background the operator cannot see.
        event.preventDefault();
        return;
      }

      const active = document.activeElement;
      // Focus outside the dialog altogether — it started on <body>, or
      // something stole it — is pulled back in rather than left there.
      if (!(active instanceof HTMLElement) || !root.contains(active)) {
        event.preventDefault();
        (event.shiftKey ? last : first).focus();
        return;
      }
      if (event.shiftKey && active === first) {
        event.preventDefault();
        last.focus();
        return;
      }
      if (!event.shiftKey && active === last) {
        event.preventDefault();
        first.focus();
      }
    }

    document.addEventListener("keydown", onKeyDown);
    return () => {
      document.removeEventListener("keydown", onKeyDown);
    };
  }, [container]);
}
