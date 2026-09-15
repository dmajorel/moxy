import type { ComponentProps } from "react";
import { fireEvent, render, screen } from "@testing-library/react";

import { LocaleProvider } from "@/i18n/locale";

import { LangToggle } from "./LangToggle";

function renderToggle(props: Partial<ComponentProps<typeof LangToggle>> = {}) {
  const onPreferenceChange = vi.fn();
  const view = render(
    <LangToggle
      preference="system"
      onPreferenceChange={onPreferenceChange}
      {...props}
    />,
  );
  return { ...view, onPreferenceChange };
}

function openMenu() {
  fireEvent.click(screen.getByRole("button", { name: /^Langue/ }));
}

describe("LangToggle", () => {
  it("names the current language in words, not by its glyph alone", () => {
    renderToggle({ preference: "en" });

    expect(
      screen.getByRole("button", { name: "Langue · English" }),
    ).toBeInTheDocument();
  });

  it("names the browser default as such", () => {
    renderToggle({ preference: "system" });

    expect(
      screen.getByRole("button", { name: "Langue · Langue du navigateur" }),
    ).toBeInTheDocument();
  });

  it("keeps the menu closed until it is asked for", () => {
    renderToggle();

    expect(screen.queryByRole("menu")).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: /^Langue/ })).toHaveAttribute(
      "aria-expanded",
      "false",
    );
  });

  it("offers the two languages and the browser default", () => {
    renderToggle();

    openMenu();

    const items = screen.getAllByRole("menuitemradio");
    expect(items.map((item) => item.textContent)).toEqual([
      "Français",
      "English",
      "Langue du navigateur",
    ]);
  });

  /*
   * The convention every language picker follows, and the only thing that lets
   * someone find their own language in an interface they cannot read: an
   * endonym stays in its own language whatever the page is in.
   */
  it("leaves each language named in its own language", () => {
    render(
      <LocaleProvider locale="en">
        <LangToggle preference="en" onPreferenceChange={vi.fn()} />
      </LocaleProvider>,
    );

    fireEvent.click(screen.getByRole("button", { name: /^Language/ }));

    const items = screen.getAllByRole("menuitemradio");
    expect(items.map((item) => item.textContent)).toEqual([
      "Français",
      "English",
      // Only the entry naming a behaviour follows the interface.
      "Browser language",
    ]);
  });

  // Without it a screen reader reads "English" with French phonetics.
  it("marks each endonym with the language it is written in", () => {
    renderToggle();

    openMenu();

    expect(screen.getByRole("menuitemradio", { name: "Français" })).toHaveAttribute(
      "lang",
      "fr",
    );
    expect(screen.getByRole("menuitemradio", { name: "English" })).toHaveAttribute(
      "lang",
      "en",
    );
    // "system" is not a language tag, and the wording is in the page's own
    // language anyway.
    expect(
      screen.getByRole("menuitemradio", { name: "Langue du navigateur" }),
    ).not.toHaveAttribute("lang");
  });

  it("marks the current language as checked", () => {
    renderToggle({ preference: "fr" });

    openMenu();

    expect(screen.getByRole("menuitemradio", { name: "Français" })).toBeChecked();
    expect(screen.getByRole("menuitemradio", { name: "English" })).not.toBeChecked();
  });

  it("reports the language that was clicked", () => {
    const { onPreferenceChange } = renderToggle();

    openMenu();
    fireEvent.click(screen.getByRole("menuitemradio", { name: "English" }));

    expect(onPreferenceChange).toHaveBeenCalledWith("en");
  });

  it("reports going back to the browser language", () => {
    const { onPreferenceChange } = renderToggle({ preference: "en" });

    openMenu();
    fireEvent.click(
      screen.getByRole("menuitemradio", { name: "Langue du navigateur" }),
    );

    expect(onPreferenceChange).toHaveBeenCalledWith("system");
  });

  it("closes and hands the focus back after a choice", () => {
    renderToggle();

    openMenu();
    fireEvent.click(screen.getByRole("menuitemradio", { name: "English" }));

    expect(screen.queryByRole("menu")).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: /^Langue/ })).toHaveFocus();
  });

  it("opens on the down arrow, on the current language", () => {
    renderToggle({ preference: "en" });

    fireEvent.keyDown(screen.getByRole("button", { name: /^Langue/ }), {
      key: "ArrowDown",
    });

    expect(screen.getByRole("menu")).toBeInTheDocument();
    expect(screen.getByRole("menuitemradio", { name: "English" })).toHaveFocus();
  });

  it("moves the focus with the arrow keys", () => {
    renderToggle({ preference: "fr" });

    openMenu();
    fireEvent.keyDown(screen.getByRole("menu"), { key: "ArrowDown" });

    expect(screen.getByRole("menuitemradio", { name: "English" })).toHaveFocus();
  });

  it("chooses the focused language on Enter", () => {
    const { onPreferenceChange } = renderToggle({ preference: "fr" });

    openMenu();
    fireEvent.keyDown(screen.getByRole("menu"), { key: "ArrowDown" });
    fireEvent.keyDown(screen.getByRole("menu"), { key: "Enter" });

    expect(onPreferenceChange).toHaveBeenCalledWith("en");
  });

  it("closes on Escape and hands the focus back, changing nothing", () => {
    const { onPreferenceChange } = renderToggle();

    openMenu();
    fireEvent.keyDown(screen.getByRole("menu"), { key: "Escape" });

    expect(screen.queryByRole("menu")).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: /^Langue/ })).toHaveFocus();
    expect(onPreferenceChange).not.toHaveBeenCalled();
  });

  it("closes on a click outside", () => {
    renderToggle();

    openMenu();
    fireEvent.mouseDown(document.body);

    expect(screen.queryByRole("menu")).not.toBeInTheDocument();
  });

  it("merges the className it receives", () => {
    const { container } = renderToggle({ className: "ml-1" });

    expect(container.firstElementChild).toHaveClass("ml-1");
  });

  it("paints itself from tokens, so the dark theme needs no change here", () => {
    const { container } = renderToggle();

    openMenu();

    expect(container.querySelector(".bg-surface-0")).not.toBeNull();
    expect(container.querySelector("[style]")).toBeNull();
  });
});
