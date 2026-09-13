import { fireEvent, render, screen } from "@testing-library/react";

import {
  AppShell,
  DEFAULT_SIDEBAR_WIDTH,
  MAX_SIDEBAR_WIDTH,
  MIN_SIDEBAR_WIDTH,
} from "./AppShell";

const STORAGE_KEY = "moxy.sidebar-width";
const HANDLE_LABEL = "Largeur du panneau de navigation";

function renderShell() {
  return render(
    <AppShell topBar={<span>top bar region</span>} sidebar={<span>sidebar region</span>}>
      <span>content region</span>
    </AppShell>,
  );
}

/** The separator between the two columns, which is also the resize handle. */
function handle() {
  return screen.getByRole("separator", { name: HANDLE_LABEL });
}

/** The width the layout actually renders, read back from the custom property. */
function renderedWidth(container: HTMLElement): string {
  const root = container.firstElementChild as HTMLElement;
  return root.style.getPropertyValue("--sidebar-width");
}

/** Drags the handle to an absolute x position and releases it. */
function drag(clientX: number) {
  fireEvent.pointerDown(handle(), { button: 0, clientX: DEFAULT_SIDEBAR_WIDTH });
  fireEvent.pointerMove(window, { clientX });
  fireEvent.pointerUp(window);
}

beforeEach(() => {
  window.localStorage.clear();
});

describe("AppShell", () => {
  it("places its three regions", () => {
    renderShell();

    expect(screen.getByText("top bar region")).toBeInTheDocument();
    expect(screen.getByText("sidebar region")).toBeInTheDocument();
    expect(screen.getByText("content region")).toBeInTheDocument();
  });

  it("exposes the two columns as aside and main", () => {
    const { container } = renderShell();

    const aside = container.querySelector("aside");
    const main = container.querySelector("main");
    expect(aside).not.toBeNull();
    expect(main).not.toBeNull();
    expect(aside).toContainElement(screen.getByText("sidebar region"));
    expect(main).toContainElement(screen.getByText("content region"));
    // The top bar sits outside both columns, so it never scrolls with them.
    expect(aside).not.toContainElement(screen.getByText("top bar region"));
    expect(main).not.toContainElement(screen.getByText("top bar region"));
  });

  it("sizes the sidebar column from the width it holds", () => {
    const { container } = renderShell();

    const grid = container.querySelector("aside")?.parentElement;
    expect(grid).toHaveClass("grid-cols-[var(--sidebar-width)_minmax(0,1fr)]");
    expect(renderedWidth(container)).toContain(`${DEFAULT_SIDEBAR_WIDTH}px`);
  });

  it("caps the sidebar at a share of the viewport, whatever the stored width", () => {
    const { container } = renderShell();

    // The second cap is expressed in CSS so a narrow window never leaves the
    // content pane without room, with no resize listener to keep in sync.
    expect(renderedWidth(container)).toBe(`min(${DEFAULT_SIDEBAR_WIDTH}px, 45vw)`);
  });

  it("lets each column scroll on its own", () => {
    const { container } = renderShell();

    expect(container.querySelector("aside")).toHaveClass("overflow-y-auto");
    expect(container.querySelector("main")).toHaveClass("overflow-y-auto");
  });

  it("offers a skip link that targets the main region", () => {
    const { container } = renderShell();

    const link = screen.getByRole("link", { name: "Aller au contenu" });
    const main = container.querySelector("main");
    expect(link).toHaveAttribute("href", `#${main?.id ?? ""}`);
    expect(main?.id).toBeTruthy();
    // Focusable by script so the jump really moves keyboard focus.
    expect(main).toHaveAttribute("tabindex", "-1");
  });

  it("keeps the skip link out of sight until it takes focus", () => {
    renderShell();

    const link = screen.getByRole("link", { name: "Aller au contenu" });
    expect(link).toHaveClass("sr-only");
    expect(link).toHaveClass("focus:not-sr-only");
  });

  it("merges the className it receives with its own classes", () => {
    const { container } = render(
      <AppShell className="border-t-[0.5px]" topBar={null} sidebar={null}>
        {null}
      </AppShell>,
    );

    const root = container.firstElementChild;
    expect(root).toHaveClass("border-t-[0.5px]");
    expect(root).toHaveClass("h-screen");
  });

  describe("resize handle", () => {
    it("describes itself as a focusable vertical separator", () => {
      renderShell();

      const separator = handle();
      expect(separator).toHaveAttribute("aria-orientation", "vertical");
      expect(separator).toHaveAttribute("aria-valuenow", String(DEFAULT_SIDEBAR_WIDTH));
      expect(separator).toHaveAttribute("aria-valuemin", String(MIN_SIDEBAR_WIDTH));
      expect(separator).toHaveAttribute("aria-valuemax", String(MAX_SIDEBAR_WIDTH));
      expect(separator).toHaveAttribute("tabindex", "0");
      expect(separator).toHaveClass("cursor-col-resize");
    });

    it("widens on the right arrow and narrows on the left arrow", () => {
      const { container } = renderShell();

      fireEvent.keyDown(handle(), { key: "ArrowRight" });
      const widened = Number(handle().getAttribute("aria-valuenow"));
      expect(widened).toBeGreaterThan(DEFAULT_SIDEBAR_WIDTH);
      expect(renderedWidth(container)).toContain(`${widened}px`);

      fireEvent.keyDown(handle(), { key: "ArrowLeft" });
      expect(handle()).toHaveAttribute("aria-valuenow", String(DEFAULT_SIDEBAR_WIDTH));
    });

    it("jumps to either bound with Home and End", () => {
      renderShell();

      fireEvent.keyDown(handle(), { key: "End" });
      expect(handle()).toHaveAttribute("aria-valuenow", String(MAX_SIDEBAR_WIDTH));

      fireEvent.keyDown(handle(), { key: "Home" });
      expect(handle()).toHaveAttribute("aria-valuenow", String(MIN_SIDEBAR_WIDTH));
    });

    it("never goes past its bounds, whatever the key repeat", () => {
      renderShell();

      for (let i = 0; i < 60; i += 1) fireEvent.keyDown(handle(), { key: "ArrowLeft" });
      expect(handle()).toHaveAttribute("aria-valuenow", String(MIN_SIDEBAR_WIDTH));

      for (let i = 0; i < 60; i += 1) fireEvent.keyDown(handle(), { key: "ArrowRight" });
      expect(handle()).toHaveAttribute("aria-valuenow", String(MAX_SIDEBAR_WIDTH));
    });

    it("leaves keys it does not handle to the browser", () => {
      renderShell();

      fireEvent.keyDown(handle(), { key: "ArrowUp" });
      fireEvent.keyDown(handle(), { key: "a" });
      expect(handle()).toHaveAttribute("aria-valuenow", String(DEFAULT_SIDEBAR_WIDTH));
    });

    it("follows the pointer while dragging", () => {
      const { container } = renderShell();

      drag(300);

      expect(handle()).toHaveAttribute("aria-valuenow", "300");
      expect(renderedWidth(container)).toContain("300px");
    });

    it("clamps a drag to the bounds instead of collapsing the sidebar", () => {
      renderShell();

      drag(-200);
      expect(handle()).toHaveAttribute("aria-valuenow", String(MIN_SIDEBAR_WIDTH));

      drag(5000);
      expect(handle()).toHaveAttribute("aria-valuenow", String(MAX_SIDEBAR_WIDTH));
    });

    it("ignores a drag started with a secondary button", () => {
      renderShell();

      fireEvent.pointerDown(handle(), { button: 2, clientX: DEFAULT_SIDEBAR_WIDTH });
      fireEvent.pointerMove(window, { clientX: 300 });

      expect(handle()).toHaveAttribute("aria-valuenow", String(DEFAULT_SIDEBAR_WIDTH));
    });

    it("stops following the pointer once released", () => {
      renderShell();

      drag(300);
      fireEvent.pointerMove(window, { clientX: 250 });

      expect(handle()).toHaveAttribute("aria-valuenow", "300");
    });

    it("returns to the nominal width on a double click", () => {
      renderShell();

      drag(320);
      fireEvent.doubleClick(handle());

      expect(handle()).toHaveAttribute("aria-valuenow", String(DEFAULT_SIDEBAR_WIDTH));
    });
  });

  describe("width persistence", () => {
    it("remembers the width a drag settled on", () => {
      renderShell();

      drag(300);

      expect(window.localStorage.getItem(STORAGE_KEY)).toBe("300");
    });

    it("remembers the width the keyboard settled on", () => {
      renderShell();

      fireEvent.keyDown(handle(), { key: "End" });

      expect(window.localStorage.getItem(STORAGE_KEY)).toBe(String(MAX_SIDEBAR_WIDTH));
    });

    it("restores the remembered width on the next visit", () => {
      window.localStorage.setItem(STORAGE_KEY, "260");

      const { container } = renderShell();

      expect(handle()).toHaveAttribute("aria-valuenow", "260");
      expect(renderedWidth(container)).toContain("260px");
    });

    it("clamps a remembered width that no longer fits the bounds", () => {
      window.localStorage.setItem(STORAGE_KEY, "9000");

      renderShell();

      expect(handle()).toHaveAttribute("aria-valuenow", String(MAX_SIDEBAR_WIDTH));
    });

    it("falls back to the nominal width when the entry makes no sense", () => {
      window.localStorage.setItem(STORAGE_KEY, "wide please");

      renderShell();

      expect(handle()).toHaveAttribute("aria-valuenow", String(DEFAULT_SIDEBAR_WIDTH));
    });

    it("survives a storage accessor that throws, as a private window does", () => {
      const getItem = vi
        .spyOn(Storage.prototype, "getItem")
        .mockImplementation(() => {
          throw new Error("access denied");
        });
      const setItem = vi
        .spyOn(Storage.prototype, "setItem")
        .mockImplementation(() => {
          throw new Error("access denied");
        });

      try {
        renderShell();
        expect(handle()).toHaveAttribute("aria-valuenow", String(DEFAULT_SIDEBAR_WIDTH));

        // Resizing still works; only remembering it does not.
        fireEvent.keyDown(handle(), { key: "End" });
        expect(handle()).toHaveAttribute("aria-valuenow", String(MAX_SIDEBAR_WIDTH));
      } finally {
        getItem.mockRestore();
        setItem.mockRestore();
      }
    });
  });
});

// The shell is what keeps a broken screen from becoming a blank page: React
// unmounts the WHOLE tree when a render throws and nothing catches it, so one
// missing field in one payload used to take the top bar and the tree with it.
describe("a screen that throws", () => {
  beforeEach(() => {
    // React logs the caught error itself; the test output is not where to
    // read it.
    vi.spyOn(console, "error").mockImplementation(() => undefined);
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  function Bomb(): never {
    throw new TypeError("Cannot read properties of undefined (reading 'ratio')");
  }

  it("leaves the top bar and the tree standing", () => {
    render(
      <AppShell topBar={<span>top bar region</span>} sidebar={<span>sidebar region</span>}>
        <Bomb />
      </AppShell>,
    );

    // The way out of the broken screen is still there.
    expect(screen.getByText("top bar region")).toBeInTheDocument();
    expect(screen.getByText("sidebar region")).toBeInTheDocument();
    // And in its place, an explanation rather than nothing.
    expect(screen.getByRole("alert")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Réessayer" })).toBeInTheDocument();
  });

  it("keeps the pane and its skip target", () => {
    render(
      <AppShell topBar={<span>top bar region</span>} sidebar={<span>sidebar region</span>}>
        <Bomb />
      </AppShell>,
    );

    expect(screen.getByRole("main")).toBeInTheDocument();
    expect(handle()).toBeInTheDocument();
  });
});
