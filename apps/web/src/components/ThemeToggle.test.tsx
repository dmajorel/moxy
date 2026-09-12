import type { ComponentProps } from "react";
import { fireEvent, render, screen } from "@testing-library/react";

import { ThemeToggle } from "./ThemeToggle";

function renderToggle(props: Partial<ComponentProps<typeof ThemeToggle>> = {}) {
  const onPreferenceChange = vi.fn();
  const view = render(
    <ThemeToggle
      preference="system"
      onPreferenceChange={onPreferenceChange}
      {...props}
    />,
  );
  return { ...view, onPreferenceChange };
}

function openMenu() {
  fireEvent.click(screen.getByRole("button", { name: /^Thème/ }));
}

describe("ThemeToggle", () => {
  it("names the current mode in words, not by its icon alone", () => {
    renderToggle({ preference: "dark" });

    expect(
      screen.getByRole("button", { name: "Thème · Sombre" }),
    ).toBeInTheDocument();
  });

  it("names the system preference as such", () => {
    renderToggle({ preference: "system" });

    expect(
      screen.getByRole("button", { name: "Thème · Système" }),
    ).toBeInTheDocument();
  });

  it("keeps the menu closed until it is asked for", () => {
    renderToggle();

    expect(screen.queryByRole("menu")).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: /^Thème/ })).toHaveAttribute(
      "aria-expanded",
      "false",
    );
  });

  it("offers the three modes, not a two-way switch", () => {
    renderToggle();

    openMenu();

    const items = screen.getAllByRole("menuitemradio");
    expect(items.map((item) => item.textContent)).toEqual([
      "Clair",
      "Sombre",
      "Système",
    ]);
  });

  it("marks the current mode as checked", () => {
    renderToggle({ preference: "light" });

    openMenu();

    expect(screen.getByRole("menuitemradio", { name: "Clair" })).toBeChecked();
    expect(screen.getByRole("menuitemradio", { name: "Sombre" })).not.toBeChecked();
  });

  it("reports the mode that was clicked", () => {
    const { onPreferenceChange } = renderToggle();

    openMenu();
    fireEvent.click(screen.getByRole("menuitemradio", { name: "Sombre" }));

    expect(onPreferenceChange).toHaveBeenCalledWith("dark");
  });

  it("reports going back to the system preference", () => {
    const { onPreferenceChange } = renderToggle({ preference: "dark" });

    openMenu();
    fireEvent.click(screen.getByRole("menuitemradio", { name: "Système" }));

    expect(onPreferenceChange).toHaveBeenCalledWith("system");
  });

  it("closes and hands the focus back after a choice", () => {
    renderToggle();

    openMenu();
    fireEvent.click(screen.getByRole("menuitemradio", { name: "Clair" }));

    expect(screen.queryByRole("menu")).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: /^Thème/ })).toHaveFocus();
  });

  it("opens on the down arrow, on the current mode", () => {
    renderToggle({ preference: "dark" });

    fireEvent.keyDown(screen.getByRole("button", { name: /^Thème/ }), {
      key: "ArrowDown",
    });

    expect(screen.getByRole("menu")).toBeInTheDocument();
    expect(screen.getByRole("menuitemradio", { name: "Sombre" })).toHaveFocus();
  });

  it("moves the focus with the arrow keys", () => {
    renderToggle({ preference: "light" });

    openMenu();
    fireEvent.keyDown(screen.getByRole("menu"), { key: "ArrowDown" });

    expect(screen.getByRole("menuitemradio", { name: "Sombre" })).toHaveFocus();
  });

  it("wraps around at the ends of the menu", () => {
    renderToggle({ preference: "light" });

    openMenu();
    fireEvent.keyDown(screen.getByRole("menu"), { key: "ArrowUp" });

    expect(screen.getByRole("menuitemradio", { name: "Système" })).toHaveFocus();
  });

  it("chooses the focused mode on Enter", () => {
    const { onPreferenceChange } = renderToggle({ preference: "light" });

    openMenu();
    fireEvent.keyDown(screen.getByRole("menu"), { key: "ArrowDown" });
    fireEvent.keyDown(screen.getByRole("menu"), { key: "Enter" });

    expect(onPreferenceChange).toHaveBeenCalledWith("dark");
  });

  it("closes on Escape and hands the focus back", () => {
    renderToggle();

    openMenu();
    fireEvent.keyDown(screen.getByRole("menu"), { key: "Escape" });

    expect(screen.queryByRole("menu")).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: /^Thème/ })).toHaveFocus();
  });

  it("changes nothing when Escape closes it", () => {
    const { onPreferenceChange } = renderToggle();

    openMenu();
    fireEvent.keyDown(screen.getByRole("menu"), { key: "Escape" });

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
