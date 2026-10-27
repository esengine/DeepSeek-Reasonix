// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import "./testkit";
import { SlottedView } from "./SlottedView";
import { arrange, headerBody, placement } from "./slots";
import type { ExtensionSurface, ExtensionViewNode } from "../port/wire";

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

const view = (pluginId: string, surfaceId: string, slot?: string, body: ExtensionViewNode[] = [{ kind: "text", value: surfaceId }]): ExtensionSurface => ({
  pluginId, surfaceId, kind: "view", view: { slot, body },
});
const ids = (list: ExtensionSurface[]) => list.map((v) => `${v.pluginId}:${v.surfaceId}`);

describe("where a view stands", () => {
  it("reads the protocol's slot names as the places this window has", () => {
    expect(placement(view("p", "a", "rail"), {})).toBe("sidebar");
    expect(placement(view("p", "a", "composer.above"), {})).toBe("composer-trailing");
    expect(placement(view("p", "a", "session.header"), {})).toBe("session-header");
    expect(placement(view("p", "a", "pane.tab"), {})).toBe("pane-tab");
  });

  it("keeps the names placements were stored under before the vocabulary grew", () => {
    expect(placement(view("p", "a", "sidebar"), {})).toBe("sidebar");
    expect(placement(view("p", "a", "composer-trailing"), {})).toBe("composer-trailing");
    expect(placement(view("p", "a", "above_prompt"), {})).toBe("composer-trailing");
    expect(placement(view("p", "a", "pane.tab"), { "p:a": "composer-trailing" })).toBe("composer-trailing");
  });

  it("lets the user overrule the extension, and falls back to what it asked when a stored place is gone", () => {
    expect(placement(view("p", "a", "pane.tab"), { "p:a": "session-header" })).toBe("session-header");
    expect(placement(view("p", "a", "pane.tab"), { "p:a": "settings.section" })).toBe("pane-tab");
    expect(placement(view("p", "a", "home.card"), {})).toBe("sidebar");
    expect(placement(view("p", "a"), {})).toBe("sidebar");
  });
});

describe("arranging every view of a pane", () => {
  it("orders a place by plugin then surface, never by when a view arrived", () => {
    const late = [view("zeta", "b", "rail"), view("alpha", "z", "rail"), view("alpha", "a", "rail"), view("a-b", "x", "rail")];
    expect(ids(arrange(late, {}).sidebar)).toEqual(["a-b:x", "alpha:a", "alpha:z", "zeta:b"]);
    expect(ids(arrange([...late].reverse(), {}).sidebar)).toEqual(["a-b:x", "alpha:a", "alpha:z", "zeta:b"]);
  });

  it("sends what a full place cannot hold to the rail rather than nowhere", () => {
    const asks = ["a", "b", "c"].map((s) => view("p", s, "composer.above"));
    const placed = arrange(asks, {});
    expect(ids(placed["composer-trailing"])).toEqual(["p:a", "p:b"]);
    expect(ids(placed.sidebar)).toEqual(["p:c"]);
    const header = arrange(["a", "b", "c", "d"].map((s) => view("p", s, "session.header")), {});
    expect(ids(header["session-header"])).toEqual(["p:a", "p:b", "p:c"]);
    expect(ids(header.sidebar)).toEqual(["p:d"]);
    const tabs = arrange(["a", "b", "c", "d", "e"].map((s) => view("p", s, "pane.tab")), {});
    expect(ids(tabs["pane-tab"])).toEqual(["p:a", "p:b", "p:c", "p:d"]);
    expect(ids(tabs.sidebar)).toEqual(["p:e"]);
  });

  it("gives every place a list even when nothing stands in it", () => {
    expect(arrange([], {})).toEqual({ sidebar: [], "composer-trailing": [], "session-header": [], "pane-tab": [] });
  });
});

describe("a view in the session header", () => {
  it("keeps text, pips, meters and buttons, and lifts them out of rows and stacks", () => {
    const body: ExtensionViewNode[] = [
      { kind: "row", children: [{ kind: "pip", tone: "ok" }, { kind: "text", value: "62%" }] },
      { kind: "stack", children: [{ kind: "meter", progress: 0.5 }, { kind: "button", label: "Sync", actionId: "sync" }] },
    ];
    expect(headerBody(body)).toEqual([
      { kind: "pip", tone: "ok" },
      { kind: "text", value: "62%" },
      { kind: "meter", progress: 0.5 },
      { kind: "button", label: "Sync", actionId: "sync" },
    ]);
  });

  it("says what a node the header cannot draw holds instead of dropping it", () => {
    expect(headerBody([
      { kind: "markdown", value: "**done**", tone: "ok" },
      { kind: "kv", key: "quota", value: "620/1000" },
      { kind: "divider" },
    ])).toEqual([
      { kind: "text", value: "**done**", tone: "ok" },
      { kind: "text", value: "quota: 620/1000" },
    ]);
  });
});

describe("moving a view", () => {
  it.each([20, 800])("keeps the menu inside a phone viewport at y=%s and follows scrolling", (top) => {
    vi.stubGlobal("innerWidth", 390);
    vi.stubGlobal("innerHeight", 844);
    render(<SlottedView ext={view("p", "a")} onAction={() => {}} onMove={() => {}} />);
    const trigger = screen.getByRole("button", { name: "移动 p 的界面" });
    const menu = screen.getByRole("menu", { hidden: true });
    let y = top;
    vi.spyOn(trigger, "getBoundingClientRect").mockImplementation(() => new DOMRect(370, y, 16, 20));
    Object.defineProperty(menu, "offsetWidth", { configurable: true, value: 286 });
    Object.defineProperty(menu, "offsetHeight", { configurable: true, value: 306 });
    const fits = () => {
      const left = parseFloat(menu.style.left), upper = parseFloat(menu.style.top);
      expect(left).toBeGreaterThanOrEqual(0);
      expect(left + menu.offsetWidth).toBeLessThanOrEqual(innerWidth);
      expect(upper).toBeGreaterThanOrEqual(0);
      expect(upper + menu.offsetHeight).toBeLessThanOrEqual(innerHeight);
      if (y === 20) expect(upper).toBeGreaterThanOrEqual(y + 20);
      else expect(upper + menu.offsetHeight).toBeLessThanOrEqual(y);
    };
    fireEvent.click(trigger);
    fits();
    y = top === 20 ? 800 : 20;
    fireEvent.scroll(window);
    fits();
  });

  it("opens outside a clipping slot and returns focus on Escape", () => {
    const { container } = render(<div style={{ overflow: "hidden" }}><SlottedView ext={view("p", "a", "session.header")} onAction={() => {}} onMove={() => {}} /></div>);
    const trigger = screen.getByRole("button", { name: "移动 p 的界面" });
    fireEvent.click(trigger);
    const menu = screen.getByRole("menu");
    expect(container.contains(menu)).toBe(false);
    expect(menu.parentElement).toBe(document.body);
    fireEvent.keyDown(document.activeElement!, { key: "Escape" });
    expect(screen.queryByRole("menu")).toBeNull();
    expect(document.activeElement).toBe(trigger);
  });

  it("offers every place this window has, under its own name, and says which one holds it", () => {
    render(<SlottedView ext={view("p", "a", "pane.tab")} onAction={() => {}} onMove={() => {}} />);
    const items = screen.getAllByRole("menuitem", { hidden: true });
    expect(items.map((b) => b.textContent)).toEqual(["扩展页签", "输入框上方", "会话标题栏", "单独的页签", "交还给插件"]);
    expect(items.map((b) => b.getAttribute("aria-current"))).toEqual([null, null, null, "true", null]);
  });

  it("dismisses on an outside press without moving the view", () => {
    const onMove = vi.fn();
    render(<SlottedView ext={view("p", "a")} onAction={() => {}} onMove={onMove} />);
    fireEvent.click(screen.getByRole("button", { name: "移动 p 的界面" }));
    fireEvent.mouseDown(document.body);
    expect(screen.queryByRole("menu")).toBeNull();
    expect(onMove).not.toHaveBeenCalled();
  });

  it("does not carry an open menu into another extension tab", () => {
    const props = { onAction: vi.fn(), onMove: vi.fn() };
    const { rerender } = render(<SlottedView ext={view("p", "a")} {...props} />);
    fireEvent.click(screen.getByRole("button", { name: "移动 p 的界面" }));
    expect(screen.getByRole("menu")).toBeTruthy();
    rerender(<SlottedView ext={view("p", "b")} {...props} />);
    expect(screen.queryByRole("menu")).toBeNull();
    expect(screen.getByRole("button", { name: "移动 p 的界面" }).getAttribute("aria-expanded")).toBe("false");
    expect(props.onMove).not.toHaveBeenCalled();
  });

  it("moves to the place picked, and hands the choice back without hiding the view", () => {
    const onMove = vi.fn();
    render(<SlottedView ext={view("p", "a")} onAction={() => {}} onMove={onMove} />);
    fireEvent.click(screen.getByRole("button", { name: "移动 p 的界面" }));
    fireEvent.click(screen.getByRole("menuitem", { name: "会话标题栏" }));
    fireEvent.click(screen.getByRole("button", { name: "移动 p 的界面" }));
    fireEvent.click(screen.getByRole("menuitem", { name: "交还给插件" }));
    expect(onMove.mock.calls).toEqual([["session-header"], [""]]);
  });

  it("draws the body it is handed, so a narrow place can restrict what it shows", async () => {
    const ext = view("p", "a", "session.header", [{ kind: "kv", key: "k", value: "v" }]);
    render(<SlottedView ext={ext} body={headerBody(ext.view!.body)} onAction={() => {}} onMove={() => {}} />);
    expect(await screen.findByText("k: v")).toBeTruthy();
  });
});

describe("a frame among the views", () => {
  const frame = (slot?: string): ExtensionSurface => ({
    pluginId: "board", surfaceId: "kanban", kind: "frame",
    frame: { frame: "board", slot, title: "Task board", fallback: "3 cards" },
  });

  it("stands where it asked, except in the one-line title bar, where it goes to the rail", () => {
    expect(placement(frame("pane.tab"), {})).toBe("pane-tab");
    expect(placement(frame("composer.above"), {})).toBe("composer-trailing");
    expect(placement(frame("session.header"), {})).toBe("sidebar");
    expect(placement(frame("pane.tab"), { "board:kanban": "session-header" })).toBe("sidebar");
  });

  it("is not offered the title bar when moved", () => {
    render(<SlottedView ext={frame("pane.tab")} onAction={() => {}} onMove={() => {}} />);
    const items = screen.getAllByRole("menuitem", { hidden: true }).filter((b) => b.dataset.action === "panel.move").map((b) => b.textContent);
    expect(items).toEqual(["扩展页签", "输入框上方", "单独的页签"]);
  });

  it("shows its fallback where no frame host is given", () => {
    render(<SlottedView ext={frame("pane.tab")} onAction={() => {}} onMove={() => {}} />);
    expect(screen.getByText("3 cards")).toBeTruthy();
  });
});
