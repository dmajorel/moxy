import type { ComponentProps } from "react";
import { fireEvent, render, screen, within } from "@testing-library/react";

import { ClusterSwitcher } from "./ClusterSwitcher";
import type { ClusterSwitcherCluster } from "./ClusterSwitcher";

const CLUSTERS: ClusterSwitcherCluster[] = [
  { id: "qual", name: "Qualification", status: "healthy" },
  { id: "pprd", name: "Préproduction", status: "healthy" },
  { id: "prod", name: "Production", status: "healthy" },
];

function degraded(): ClusterSwitcherCluster[] {
  return [
    CLUSTERS[0]!,
    { id: "pprd", name: "Préproduction", status: "degraded" },
    CLUSTERS[2]!,
  ];
}

function renderSwitcher(
  props: Partial<ComponentProps<typeof ClusterSwitcher>> = {},
) {
  const onSelect = vi.fn();
  render(
    <ClusterSwitcher
      clusters={CLUSTERS}
      selectedId={null}
      onSelect={onSelect}
      {...props}
    />,
  );
  return { onSelect, button: screen.getByRole("button") };
}

/** The aggregated dot lives inside the trigger button. */
function triggerDot(button: HTMLElement): HTMLElement {
  const dot = button.querySelector('[role="img"]');
  expect(dot).not.toBeNull();
  return dot as HTMLElement;
}

describe("ClusterSwitcher", () => {
  it("counts the clusters in the plural", () => {
    const { button } = renderSwitcher();

    expect(button).toHaveTextContent("3 clusters");
  });

  it("counts a lone cluster in the singular", () => {
    const { button } = renderSwitcher({ clusters: [CLUSTERS[0]!] });

    expect(button).toHaveTextContent("1 cluster");
    expect(button).not.toHaveTextContent("clusters");
  });

  it("names the selected cluster instead of the count", () => {
    const { button } = renderSwitcher({ selectedId: "prod" });

    expect(button).toHaveTextContent("Production");
  });

  it("paints the aggregated dot green when every cluster is healthy", () => {
    const { button } = renderSwitcher();

    expect(triggerDot(button)).toHaveClass("bg-success");
  });

  it("paints the aggregated dot amber as soon as one cluster is degraded", () => {
    const { button } = renderSwitcher({ clusters: degraded() });

    expect(triggerDot(button)).toHaveClass("bg-warning");
  });

  it("exposes the menu attributes on the trigger", () => {
    const { button } = renderSwitcher();

    expect(button).toHaveAttribute("aria-haspopup", "menu");
    expect(button).toHaveAttribute("aria-expanded", "false");

    fireEvent.click(button);

    expect(button).toHaveAttribute("aria-expanded", "true");
    expect(screen.getByRole("menu")).toBeInTheDocument();
  });

  it("lists every cluster under an entry for all of them", () => {
    const { button } = renderSwitcher();

    fireEvent.click(button);

    const items = within(screen.getByRole("menu")).getAllByRole("menuitemradio");
    expect(items.map((item) => item.textContent)).toEqual([
      "Tous les clusters",
      "Qualification",
      "Préproduction",
      "Production",
    ]);
    expect(items[0]).toHaveAttribute("aria-checked", "true");
  });

  it("reports null when all the clusters are picked", () => {
    const { button, onSelect } = renderSwitcher({ selectedId: "qual" });

    fireEvent.click(button);
    fireEvent.click(screen.getByRole("menuitemradio", { name: /Tous les clusters/ }));

    expect(onSelect).toHaveBeenCalledWith(null);
    expect(screen.queryByRole("menu")).not.toBeInTheDocument();
  });

  it("reports the id of the cluster that is picked", () => {
    const { button, onSelect } = renderSwitcher();

    fireEvent.click(button);
    fireEvent.click(screen.getByRole("menuitemradio", { name: /Préproduction/ }));

    expect(onSelect).toHaveBeenCalledWith("pprd");
  });

  it("closes on a click outside", () => {
    const { button } = renderSwitcher();

    fireEvent.click(button);
    expect(screen.getByRole("menu")).toBeInTheDocument();

    fireEvent.mouseDown(document.body);

    expect(screen.queryByRole("menu")).not.toBeInTheDocument();
    expect(button).toHaveFocus();
  });

  it("closes on Escape and gives the focus back to the trigger", () => {
    const { button } = renderSwitcher();

    fireEvent.click(button);
    fireEvent.keyDown(screen.getByRole("menu"), { key: "Escape" });

    expect(screen.queryByRole("menu")).not.toBeInTheDocument();
    expect(button).toHaveFocus();
  });

  it("closes when the trigger is clicked again", () => {
    const { button } = renderSwitcher();

    fireEvent.click(button);
    fireEvent.click(button);

    expect(screen.queryByRole("menu")).not.toBeInTheDocument();
  });

  it("opens with the arrow keys and focuses the selected entry", () => {
    const { button } = renderSwitcher({ selectedId: "pprd" });

    fireEvent.keyDown(button, { key: "ArrowDown" });

    expect(screen.getByRole("menu")).toBeInTheDocument();
    expect(screen.getByRole("menuitemradio", { name: /Préproduction/ })).toHaveFocus();
  });

  it("moves the focus with the arrow keys and selects with Enter", () => {
    const { button, onSelect } = renderSwitcher();

    fireEvent.click(button);
    const menu = screen.getByRole("menu");
    expect(screen.getByRole("menuitemradio", { name: /Tous les clusters/ })).toHaveFocus();

    fireEvent.keyDown(menu, { key: "ArrowDown" });
    expect(screen.getByRole("menuitemradio", { name: /Qualification/ })).toHaveFocus();

    fireEvent.keyDown(menu, { key: "ArrowDown" });
    fireEvent.keyDown(menu, { key: "ArrowUp" });
    expect(screen.getByRole("menuitemradio", { name: /Qualification/ })).toHaveFocus();

    fireEvent.keyDown(menu, { key: "Enter" });

    expect(onSelect).toHaveBeenCalledTimes(1);
    expect(onSelect).toHaveBeenCalledWith("qual");
    expect(button).toHaveFocus();
  });

  it("wraps around at both ends of the menu", () => {
    const { button } = renderSwitcher();

    fireEvent.click(button);
    const menu = screen.getByRole("menu");

    fireEvent.keyDown(menu, { key: "ArrowUp" });
    expect(screen.getByRole("menuitemradio", { name: /Production/ })).toHaveFocus();

    fireEvent.keyDown(menu, { key: "ArrowDown" });
    expect(screen.getByRole("menuitemradio", { name: /Tous les clusters/ })).toHaveFocus();
  });

  it("merges the className it receives", () => {
    const { container } = render(
      <ClusterSwitcher
        clusters={CLUSTERS}
        selectedId={null}
        onSelect={vi.fn()}
        className="ml-2"
      />,
    );

    expect(container.firstElementChild).toHaveClass("ml-2");
  });
});
