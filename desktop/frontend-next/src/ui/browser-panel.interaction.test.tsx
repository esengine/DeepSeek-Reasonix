// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import type { BrowserTab } from "../port/port";

// The page never draws the agent's browser: it reserves a rectangle and tells
// the shell which page to draw over it. What is held here is what it tells the
// shell, since nothing in the DOM would show a wrong answer.

const PICTURE = "data:image/jpeg;base64,AAAA";

const bridge = {
  shell: "electron",
  platform: "darwin",
  titleBar: false,
  showBrowserView: vi.fn(async () => {}),
  hideBrowserView: vi.fn(async () => {}),
  freezeBrowserView: vi.fn(async () => PICTURE),
  controlBrowserView: vi.fn(async () => {}),
  navigateBrowserView: vi.fn(async () => true),
};

const tabs: BrowserTab[] = [
  { id: "t1", target: "view-a", url: "http://127.0.0.1:5173/", title: "App", active: true },
  { id: "t2", target: "view-b", url: "https://example.com/", title: "Docs", active: false },
];

const RECT = { x: 40, y: 60, width: 800, height: 500 };

// What hit testing finds above the slot at every point; the slot and the
// document under it are added by the stub, as a browser would.
let above: Element[] = [];

function layer(style: string, text = ""): HTMLElement {
  const el = document.createElement("div");
  el.setAttribute("style", `position:fixed;inset:0;${style}`);
  el.textContent = text;
  document.body.appendChild(el);
  return el;
}

async function settle(ms = 60) {
  await act(async () => {
    await Promise.resolve();
    vi.advanceTimersByTime(ms);
    await Promise.resolve();
  });
}

async function panel(props: { tabs: BrowserTab[]; shown: boolean }) {
  vi.resetModules();
  (window as unknown as { reasonixHost: typeof bridge }).reasonixHost = bridge;
  const { BrowserPanel } = await import("./BrowserPanel");
  return render(<BrowserPanel {...props} />);
}

beforeEach(() => {
  for (const fn of [bridge.showBrowserView, bridge.hideBrowserView, bridge.freezeBrowserView, bridge.controlBrowserView, bridge.navigateBrowserView]) fn.mockClear();
  bridge.freezeBrowserView.mockImplementation(async () => PICTURE);
  above = [];
  vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout", "setInterval", "clearInterval"], shouldAdvanceTime: true });
  vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockImplementation(function (this: HTMLElement) {
    const slot = this.classList.contains("bview");
    const [left, top, width, height] = slot ? [RECT.x, RECT.y, RECT.width, RECT.height] : [0, 0, 1024, 768];
    return { left, top, width, height, right: left + width, bottom: top + height, x: left, y: top } as DOMRect;
  });
  // jsdom computes no pseudo-element style; none is what these layers have.
  const computed = window.getComputedStyle.bind(window);
  vi.spyOn(window, "getComputedStyle").mockImplementation((el, pseudo) =>
    pseudo ? ({ content: "none" } as CSSStyleDeclaration) : computed(el),
  );
  document.elementsFromPoint = () => {
    const slot = document.querySelector(".bview");
    return [...above, ...(slot ? [slot, document.body, document.documentElement] : [document.body, document.documentElement])];
  };
});

afterEach(() => {
  vi.useRealTimers();
  cleanup();
  document.body.innerHTML = "";
  document.documentElement.removeAttribute("data-vt");
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

describe("which page the shell draws, and where", () => {
  it("draws the active page over the reserved rectangle, and the one picked after", async () => {
    await panel({ tabs, shown: true });
    expect(bridge.showBrowserView).toHaveBeenLastCalledWith("view-a", RECT);
    await userEvent.click(screen.getByRole("tab", { name: "Docs" }));
    expect(bridge.showBrowserView).toHaveBeenLastCalledWith("view-b", RECT);
  });

  it("draws nothing for a pane that is not on screen", async () => {
    await panel({ tabs, shown: false });
    expect(bridge.showBrowserView).not.toHaveBeenCalled();
    expect(bridge.hideBrowserView).toHaveBeenCalled();
  });

  it("puts the page away when the panel goes", async () => {
    const { unmount } = await panel({ tabs, shown: true });
    bridge.hideBrowserView.mockClear();
    unmount();
    expect(bridge.hideBrowserView).toHaveBeenCalled();
  });

  it("sends the person's controls and typed address to the page on screen", async () => {
    await panel({ tabs, shown: true });
    await userEvent.click(screen.getByRole("button", { name: "后退" }));
    expect(bridge.controlBrowserView).toHaveBeenCalledWith("view-a", "back");
    const field = screen.getByRole("textbox", { name: "网址" });
    await userEvent.clear(field);
    await userEvent.type(field, "localhost:3000{Enter}");
    expect(bridge.navigateBrowserView).toHaveBeenCalledWith("view-a", "localhost:3000");
  });

  it("never tells the person the page is paused", async () => {
    await panel({ tabs, shown: true });
    expect(document.querySelector(".bview")?.textContent).toBe("");
  });
});

describe("a layer the page draws over the view", () => {
  it("freezes the page to its last picture while a visible layer covers it", async () => {
    await panel({ tabs, shown: true });
    bridge.showBrowserView.mockClear();
    above = [layer("background:rgb(0 0 0 / .4)")];
    await settle();
    expect(bridge.freezeBrowserView).toHaveBeenCalledTimes(1);
    expect(bridge.showBrowserView).not.toHaveBeenCalled();
    expect(document.querySelector(".bview img")?.getAttribute("src")).toBe(PICTURE);
  });

  it("asks for one picture however often the covered page is checked", async () => {
    await panel({ tabs, shown: true });
    above = [layer("background:#000")];
    await settle();
    for (let i = 0; i < 5; i++) {
      document.body.appendChild(document.createElement("i"));
      await settle(500);
    }
    expect(bridge.freezeBrowserView).toHaveBeenCalledTimes(1);
  });

  it.each([
    ["at opacity 0", "background:#000;opacity:0"],
    ["inside a parent at opacity 0", null],
    ["hidden", "background:#000;visibility:hidden"],
    ["painting nothing, only catching clicks", ""],
  ])("does not count a layer %s", async (_name, style) => {
    await panel({ tabs, shown: true });
    if (style === null) {
      const parent = layer("opacity:0");
      const child = document.createElement("div");
      child.setAttribute("style", "background:#000");
      parent.appendChild(child);
      above = [child, parent];
    } else {
      above = [layer(style)];
    }
    await settle();
    expect(bridge.freezeBrowserView).not.toHaveBeenCalled();
    expect(bridge.showBrowserView).toHaveBeenLastCalledWith("view-a", RECT);
  });

  it("does not count the slot's own content or the columns around it", async () => {
    await panel({ tabs, shown: true });
    const inner = document.createElement("span");
    document.querySelector(".bview")!.appendChild(inner);
    const column = document.querySelector(".bpanel")!;
    above = [inner];
    await settle();
    document.elementsFromPoint = () => [column, document.querySelector(".bview")!, document.body];
    await settle();
    expect(bridge.freezeBrowserView).not.toHaveBeenCalled();
  });

  it("draws the live page again when the layer leaves by a transition, with no DOM change", async () => {
    await panel({ tabs, shown: true });
    const veil = layer("background:#000");
    above = [veil];
    await settle();
    bridge.showBrowserView.mockClear();
    veil.style.opacity = "0";
    await settle(0);
    bridge.showBrowserView.mockClear();
    above = [];
    await act(async () => {
      document.dispatchEvent(new Event("transitionend"));
      vi.advanceTimersByTime(60);
    });
    expect(bridge.showBrowserView).toHaveBeenLastCalledWith("view-a", RECT);
    expect(document.querySelector(".bview img")).toBeNull();
  });

  it("draws the live page again when nothing at all says the layer left", async () => {
    await panel({ tabs, shown: true });
    above = [layer("background:#000")];
    await settle();
    bridge.showBrowserView.mockClear();
    above = [];
    await settle(500);
    expect(bridge.showBrowserView).toHaveBeenLastCalledWith("view-a", RECT);
  });

  it("drops a picture that arrives after the page is live again", async () => {
    let answer: (picture: string) => void = () => {};
    bridge.freezeBrowserView.mockImplementation(() => new Promise<string>((resolve) => (answer = resolve)));
    await panel({ tabs, shown: true });
    above = [layer("background:#000")];
    await settle();
    above = [];
    await settle(500);
    await act(async () => answer(PICTURE));
    expect(document.querySelector(".bview img")).toBeNull();
  });

  it("freezes the page while a view transition runs, and draws it once it ends", async () => {
    document.documentElement.setAttribute("data-vt", "pane");
    await panel({ tabs, shown: true });
    expect(bridge.showBrowserView).not.toHaveBeenCalled();
    expect(bridge.freezeBrowserView).toHaveBeenCalled();
    await act(async () => {
      document.documentElement.removeAttribute("data-vt");
      await Promise.resolve();
      vi.advanceTimersByTime(60);
    });
    expect(bridge.showBrowserView).toHaveBeenLastCalledWith("view-a", RECT);
  });
});

describe("occluded", () => {
  it("answers from what paints over the probes, not from the topmost element", async () => {
    const { occluded } = await import("./occlusion");
    const slot = document.createElement("div");
    const inner = document.createElement("span");
    slot.appendChild(inner);
    document.body.appendChild(slot);
    const box = { x: 0, y: 0, width: 400, height: 400 };
    const ghost = layer("background:#000;opacity:0");
    const catcher = layer("");
    const card = layer("background:#fff");
    expect(occluded(slot, box, () => [inner, slot, document.body])).toBe(false);
    expect(occluded(slot, box, () => [ghost, catcher, slot, document.body])).toBe(false);
    expect(occluded(slot, box, () => [document.body, slot])).toBe(false);
    expect(occluded(slot, box, (x, y) => (x > 300 && y > 300 ? [card, slot] : [slot]))).toBe(true);
  });

  it("lets a layer reach a few pixels into the edge, as a gutter's grip does", async () => {
    const { occluded } = await import("./occlusion");
    const slot = document.createElement("div");
    document.body.appendChild(slot);
    const grip = layer("background:#888");
    const box = { x: 100, y: 0, width: 400, height: 400 };
    expect(occluded(slot, box, (x) => (x < 105 ? [grip, slot] : [slot]))).toBe(false);
    expect(occluded(slot, box, (x) => (x < 140 ? [grip, slot] : [slot]))).toBe(true);
  });
});
