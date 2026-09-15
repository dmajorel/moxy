import type { ComponentProps } from "react";
import { fireEvent, render, screen } from "@testing-library/react";

import { TopBar } from "./TopBar";
import type { AlertEntry } from "./AlertsPanel";
import type { ClusterSwitcherCluster } from "./ClusterSwitcher";

const CLUSTERS: ClusterSwitcherCluster[] = [
  { id: "qual", name: "Qualification", status: "healthy" },
  { id: "pprd", name: "Préproduction", status: "degraded" },
];

/** One entry of the bell's panel, for the cluster of the given id. */
function alert(clusterId: string): AlertEntry {
  const cluster = CLUSTERS.find((entry) => entry.id === clusterId);
  return {
    clusterId,
    clusterName: cluster?.name ?? clusterId,
    status: cluster?.status ?? "healthy",
    alert: { kind: "memory_high", ratio: 0.92, nodes: ["pve-1"] },
  };
}

function renderTopBar(props: Partial<ComponentProps<typeof TopBar>> = {}) {
  const onValueChange = vi.fn();
  const onSelectCluster = vi.fn();
  const onThemePreferenceChange = vi.fn();
  const onLangPreferenceChange = vi.fn();
  const view = render(
    <TopBar
      clusters={CLUSTERS}
      selectedClusterId={null}
      onSelectCluster={onSelectCluster}
      value=""
      onValueChange={onValueChange}
      themePreference="system"
      onThemePreferenceChange={onThemePreferenceChange}
      langPreference="system"
      onLangPreferenceChange={onLangPreferenceChange}
      alerts={[]}
      {...props}
    />,
  );
  return {
    ...view,
    onValueChange,
    onSelectCluster,
    onThemePreferenceChange,
    onLangPreferenceChange,
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
  it("renders the wordmark, the switcher, the search and the bell", () => {
    renderTopBar();

    expect(screen.getByText("moxy")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /2 clusters/ })).toBeInTheDocument();
    // Tasks are not searchable: the field filters the tree, and the tree holds
    // no task. Promising one would be the same mistake as a dead button.
    expect(
      screen.getByPlaceholderText("Rechercher une VM ou un nœud…"),
    ).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Notifications" })).toBeInTheDocument();
  });

  // The handoff draws an avatar; it showed a "?" with the tooltip
  // "Authentification non configurée" -- a control standing for an identity
  // that does not exist. It comes back the day there is a name to put in it.
  it("shows no avatar while there is no identity", () => {
    renderTopBar();

    expect(screen.queryByTitle(/Authentification/)).toBeNull();
    expect(screen.queryByText("?")).toBeNull();
  });

  it("reserves the brand colour for the logo mark", () => {
    const { container } = renderTopBar();

    const logo = container.querySelector("svg.text-brand");
    expect(logo).not.toBeNull();
    expect(container.querySelectorAll(".text-brand")).toHaveLength(1);
  });

  it("hides the logo from assistive technology, the wordmark carrying the name", () => {
    const { container } = renderTopBar();

    expect(container.querySelector("svg.text-brand")).toHaveAttribute(
      "aria-hidden",
      "true",
    );
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
    renderTopBar({ alerts: [alert("qual"), alert("pprd"), alert("pprd")] });

    const bell = screen.getByRole("button", { name: "Notifications · 3 alertes" });
    expect(bell).toHaveTextContent("3");
  });

  it("counts a lone alert in the singular", () => {
    renderTopBar({ alerts: [alert("qual")] });

    expect(
      screen.getByRole("button", { name: "Notifications · 1 alerte" }),
    ).toBeInTheDocument();
  });

  it("draws no counter without an alert", () => {
    renderTopBar();

    const bell = screen.getByRole("button", { name: "Notifications" });
    expect(bell).toHaveTextContent("");
  });

  // The bell used to be a <button> whose onClick was never supplied: it showed
  // a number and did nothing. An operator concludes the tool is broken.
  it("opens the alerts when the bell is clicked", () => {
    renderTopBar({ alerts: [alert("pprd")] });

    fireEvent.click(screen.getByRole("button", { name: /Notifications/ }));

    expect(screen.getByRole("menu", { name: "Alertes" })).toBeInTheDocument();
    expect(screen.getByRole("menuitem")).toHaveTextContent("Préproduction");
  });

  it("goes to the cluster an alert belongs to", () => {
    const { onSelectCluster } = renderTopBar({ alerts: [alert("pprd")] });

    fireEvent.click(screen.getByRole("button", { name: /Notifications/ }));
    fireEvent.click(screen.getByRole("menuitem"));

    expect(onSelectCluster).toHaveBeenCalledWith("pprd");
  });

  it("carries the theme control", () => {
    renderTopBar({ themePreference: "dark" });

    expect(
      screen.getByRole("button", { name: "Thème · Sombre" }),
    ).toBeInTheDocument();
  });

  it("forwards the theme choice", () => {
    const { onThemePreferenceChange } = renderTopBar();

    fireEvent.click(screen.getByRole("button", { name: /^Thème/ }));
    fireEvent.click(screen.getByRole("menuitemradio", { name: "Sombre" }));

    expect(onThemePreferenceChange).toHaveBeenCalledWith("dark");
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
