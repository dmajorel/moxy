import { render } from "@testing-library/react";

import { ClusterAccent } from "./ClusterAccent";

/** The mark, which is the only element the component ever renders. */
function accentOf(container: HTMLElement): HTMLElement | null {
  return container.querySelector("span");
}

describe("ClusterAccent", () => {
  it("paints the configured colour through a custom property", () => {
    const { container } = render(<ClusterAccent color="#7C5CD6" />);

    const accent = accentOf(container);
    expect(accent).not.toBeNull();
    // The value arrives as data; the utility that consumes it is a literal, so
    // Tailwind can see it. A class built by concatenation never gets generated.
    expect(accent?.style.getPropertyValue("--cluster-accent")).toBe("#7C5CD6");
    expect(accent).toHaveClass("bg-[var(--cluster-accent)]");
  });

  it("draws nothing when the cluster declares no accent", () => {
    const { container } = render(<ClusterAccent color={null} />);

    expect(accentOf(container)).toBeNull();
  });

  it("draws nothing when the field is absent altogether", () => {
    const { container } = render(<ClusterAccent />);

    expect(accentOf(container)).toBeNull();
  });

  it.each(["red", "#abc", "#12345", "#1234567", "var(--surface-0)", ""])(
    "refuses %s, which the backend contract does not allow",
    (color) => {
      const { container } = render(<ClusterAccent color={color} />);

      expect(accentOf(container)).toBeNull();
    },
  );

  it("is decorative: the cluster name beside it is the text equivalent", () => {
    const { container } = render(<ClusterAccent color="#7C5CD6" />);

    const accent = accentOf(container);
    expect(accent).toHaveAttribute("aria-hidden", "true");
    expect(accent).not.toHaveAttribute("role");
    expect(accent).not.toHaveAttribute("aria-label");
  });

  it("is a rounded square, never the round dot of a status", () => {
    const { container } = render(<ClusterAccent color="#7C5CD6" />);

    const accent = accentOf(container);
    expect(accent).toHaveClass("size-2");
    expect(accent).toHaveClass("rounded-[2px]");
    expect(accent).not.toHaveClass("rounded-full");
  });

  it("merges the className it receives with its own classes", () => {
    const { container } = render(<ClusterAccent color="#7C5CD6" className="mr-1" />);

    const accent = accentOf(container);
    expect(accent).toHaveClass("mr-1");
    expect(accent).toHaveClass("size-2");
  });
});
