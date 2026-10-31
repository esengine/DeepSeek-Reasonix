// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, render } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import type { BrowserTab } from "../port/port";
import type { PickedElement } from "../port/host";

// The picker hands the element to the chat and copies its selector; the page
// it was picked in is the shell's, so what is held here is what this panel
// asks the shell for and what it does with the answer.

const ELEMENT: NonNullable<PickedElement["element"]> = {
  pageUrl: "https://github.com/jf200s/A-eassion",
  pageTitle: "GitHub",
  tagName: "img",
  role: "img",
  accessibleName: "logo",
  selector: "main > div.hero > img",
  xpath: "/html[1]/body[1]/main[1]/div[1]/img[1]",
  text: "",
  nearbyText: "This is not the web page you are looking for.",
  htmlExcerpt: '<img src="h.png" alt="logo">',
  attributes: { src: "h.png", alt: "logo" },
  rect: { x: 8, y: 260, width: 732, height: 249 },
  style: { color: "#1F2328", display: "block", fontFamily: '"Mona Sans"', fontSize: "14px", fontWeight: "400" },
  capturedAt: 1700000000000,
};

const tabs: BrowserTab[] = [
  { id: "t1", target: "view-a", url: ELEMENT.pageUrl, title: "GitHub", active: true },
  { id: "t2", target: "view-b", url: "https://example.test/", title: "Other", active: false },
];

let shortcut: (ask: { targetId?: string }) => void = () => {};

const bridge = {
  shell: "electron",
  platform: "windows",
  titleBar: false,
  showBrowserView: vi.fn(async () => {}),
  hideBrowserView: vi.fn(async () => {}),
  freezeBrowserView: vi.fn(async () => ""),
  controlBrowserView: vi.fn(async () => {}),
  navigateBrowserView: vi.fn(async () => ""),
  onBrowserLoadState: vi.fn(() => () => {}),
  onBrowserPickShortcut: vi.fn((listener: (ask: { targetId?: string }) => void) => {
    shortcut = listener;
    return () => {};
  }),
  pickBrowserElement: vi.fn(async (): Promise<PickedElement> => ({ cancelled: false, element: ELEMENT })),
  cancelBrowserPick: vi.fn(async () => true),
  openExternal: vi.fn(),
};

async function mount(onAddToChat: (reference: string) => void = () => {}) {
  vi.resetModules();
  (window as unknown as { reasonixHost: typeof bridge }).reasonixHost = bridge;
  const { BrowserPanel } = await import("./BrowserPanel");
  return { view: render(<BrowserPanel tabs={tabs} shown onAddToChat={onAddToChat} />), BrowserPanel };
}

const pickButton = () => document.querySelector('[data-action="browser.pick"]') as HTMLButtonElement;
const noteRow = () => document.querySelector('[data-action="browser.picked"]');

beforeEach(() => {
  for (const fn of [bridge.pickBrowserElement, bridge.cancelBrowserPick, bridge.onBrowserPickShortcut]) fn.mockClear();
  bridge.pickBrowserElement.mockImplementation(async () => ({ cancelled: false, element: ELEMENT }));
  bridge.cancelBrowserPick.mockImplementation(async () => true);
});

afterEach(cleanup);

describe("picking an element from the agent's page", () => {
  it("asks the shell for the page's picker and hands what came back to the chat", async () => {
    const user = userEvent.setup();
    const onAddToChat = vi.fn();
    await mount(onAddToChat);

    await user.click(pickButton());
    expect(bridge.pickBrowserElement).toHaveBeenCalledWith("view-a", expect.objectContaining({ color: "颜色", background: "背景" }));

    const reference = onAddToChat.mock.calls[0]?.[0] as string;
    expect(reference).toContain("main > div.hero > img");
    expect(reference).toContain("browser_act eval");
    expect(reference).toContain("size: 732×249");
    expect(reference).toContain('<img src="h.png" alt="logo">');
    expect(noteRow()?.textContent).toContain("img 732×249");
  });

  it("drops the note when another tab is shown", async () => {
    const user = userEvent.setup();
    const onAddToChat = vi.fn();
    await mount(onAddToChat);
    await user.click(pickButton());
    expect(noteRow()?.textContent).toContain("img 732×249");

    const tabButtons = document.querySelectorAll('[data-action="browser.tab"]');
    await user.click(tabButtons[1] as HTMLElement);
    expect(noteRow()).toBeNull();
  });

  it("leaves the chat alone when the person picks nothing", async () => {
    const user = userEvent.setup();
    const onAddToChat = vi.fn();
    bridge.pickBrowserElement.mockImplementation(async () => ({ cancelled: true }));
    await mount(onAddToChat);

    await user.click(pickButton());
    expect(onAddToChat).not.toHaveBeenCalled();
    expect(noteRow()).toBeNull();
  });

  it("starts and stops a pick from the keyboard, and Escape stops one too", async () => {
    const user = userEvent.setup();
    const onAddToChat = vi.fn();
    await mount(onAddToChat);

    await user.keyboard("{Control>}{Shift>}c{/Shift}{/Control}");
    expect(bridge.pickBrowserElement).toHaveBeenCalledTimes(1);
    expect(onAddToChat).toHaveBeenCalledTimes(1);

    // While the page holds the keyboard the press never reaches this page, so
    // the shell forwards it; the second press puts the picker away.
    act(() => shortcut({ targetId: "view-a" }));
    expect(bridge.pickBrowserElement).toHaveBeenCalledTimes(2);

    await user.keyboard("{Escape}");
    expect(bridge.cancelBrowserPick).toHaveBeenCalledWith("view-a");
  });

  it("ignores the shell's key for another page", async () => {
    await mount();
    act(() => shortcut({ targetId: "view-b" }));
    expect(bridge.pickBrowserElement).not.toHaveBeenCalled();
  });
});
