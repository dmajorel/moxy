import type { ComponentProps } from "react";
import { fireEvent, render, screen } from "@testing-library/react";

import { TopBar } from "./TopBar";
import type { ClusterSwitcherCluster } from "./ClusterSwitcher";

const CLUSTERS: ClusterSwitcherCluster[] = [
  { id: "qual", name: "Qualification", status: "healthy" },
  { id: "pprd", name: "Préproduction", status: "degraded" },
];

function renderTopBar(props: Partial<ComponentProps<typeof TopBar>> = {}) {
  const onValueChange = vi.fn();
  const onSelectCluster = vi.fn();
  const view = render(
    <TopBar
      clusters={CLUSTERS}
      selectedClusterId={null}
      onSelectCluster={onSelectCluster}
      value=""
      onValueChange={onValueChange}
      userInitials="ro"
      {...props}
    />,
  );
  return {
    ...view,
    onValueChange,
    onSelectCluster,
    search: screen.getByRole("searchbox", { name: "Recherche globale" }),
  };
}

/** navigator.platform is a prototype getter in jsdom; shadow it per test. */
function stubPlatform(platform: string) {
  const original = Object.getOwnPropertyDescriptor(navigator, "platform");
  Object.defineProperty(navigator, "platform", {
    value: platform,
    configurable: true,
  });
  return () => {
    if (original === undefined) {
      Reflect.deleteProperty(navigator, "platform");
    } else {
      Object.defineProperty(navigator, "platform", original);
    }
  };
}

describe("TopBar", () => {
  it("renders the wordmark, the switcher, the search, the bell and the avatar", () => {
    renderTopBar({ userName: "Romain Oster" });

    expect(screen.getByText("moxy")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /2 clusters/ })).toBeInTheDocument();
    expect(
      screen.getByPlaceholderText("Rechercher une VM, un nœud, une tâche…"),
    ).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Notifications" })).toBeInTheDocument();
    expect(screen.getByTitle("Romain Oster")).toHaveTextContent("ro");
  });

  it("reserves the brand colour for the logo square", () => {
    const { container } = renderTopBar();

    const logo = container.querySelector(".bg-brand");
    expect(logo).not.toBeNull();
    expect(container.querySelectorAll(".bg-brand")).toHaveLength(1);
  });

  it("shows the version when it is given", () => {
    renderTopBar({ version: "0.1.0" });

    expect(screen.getByText("0.1.0")).toBeInTheDocument();
  });

  it("shows no version when none is given", () => {
    renderTopBar();

    expect(screen.queryByText(/^\d+\.\d+\.\d+$/)).not.toBeInTheDocument();
  });

  it("reports what is typed in the search field", () => {
    const { search, onValueChange } = renderTopBar();

    fireEvent.change(search, { target: { value: "airflow" } });

    expect(onValueChange).toHaveBeenCalledWith("airflow");
  });

  it("stays controlled by its value prop", () => {
    const { search } = renderTopBar({ value: "103" });

    expect(search).toHaveValue("103");
  });

  it("focuses the search field on Ctrl+K", () => {
    const { search } = renderTopBar();

    expect(search).not.toHaveFocus();
    fireEvent.keyDown(window, { key: "k", ctrlKey: true });

    expect(search).toHaveFocus();
  });

  it("focuses the search field on Meta+K", () => {
    const { search } = renderTopBar();

    fireEvent.keyDown(window, { key: "k", metaKey: true });

    expect(search).toHaveFocus();
  });

  it("ignores a bare K", () => {
    const { search } = renderTopBar();

    fireEvent.keyDown(window, { key: "k" });

    expect(search).not.toHaveFocus();
  });

  it("leaves the search field on Escape", () => {
    const { search } = renderTopBar();

    fireEvent.keyDown(window, { key: "k", ctrlKey: true });
    expect(search).toHaveFocus();

    fireEvent.keyDown(search, { key: "Escape" });

    expect(search).not.toHaveFocus();
  });

  it("hints at Ctrl+K away from Apple platforms", () => {
    const restore = stubPlatform("Linux x86_64");
    try {
      renderTopBar();
      expect(screen.getByText("Ctrl+K")).toBeInTheDocument();
    } finally {
      restore();
    }
  });

  it("hints at ⌘K on an Apple platform", () => {
    const restore = stubPlatform("MacIntel");
    try {
      renderTopBar();
      expect(screen.getByText("⌘K")).toBeInTheDocument();
    } finally {
      restore();
    }
  });

  it("counts the alerts on the bell", () => {
    renderTopBar({ alertCount: 3 });

    const bell = screen.getByRole("button", { name: "Notifications · 3 alertes" });
    expect(bell).toHaveTextContent("3");
  });

  it("counts a lone alert in the singular", () => {
    renderTopBar({ alertCount: 1 });

    expect(
      screen.getByRole("button", { name: "Notifications · 1 alerte" }),
    ).toBeInTheDocument();
  });

  it("draws no counter without an alert", () => {
    renderTopBar({ alertCount: 0 });

    const bell = screen.getByRole("button", { name: "Notifications" });
    expect(bell).toHaveTextContent("");
  });

  it("calls back when the bell is clicked", () => {
    const onAlertsClick = vi.fn();
    renderTopBar({ alertCount: 2, onAlertsClick });

    fireEvent.click(screen.getByRole("button", { name: /Notifications/ }));

    expect(onAlertsClick).toHaveBeenCalledTimes(1);
  });

  it("forwards the cluster selection", () => {
    const { onSelectCluster } = renderTopBar();

    fireEvent.click(screen.getByRole("button", { name: /2 clusters/ }));
    fireEvent.click(screen.getByRole("menuitemradio", { name: /Qualification/ }));

    expect(onSelectCluster).toHaveBeenCalledWith("qual");
  });

  it("merges the className it receives", () => {
    const { container } = renderTopBar({ className: "sticky" });

    expect(container.firstElementChild).toHaveClass("sticky");
    expect(container.firstElementChild).toHaveClass("bg-surface-2");
  });
});
