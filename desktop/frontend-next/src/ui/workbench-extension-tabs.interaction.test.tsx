// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import "./testkit";
import { WorkbenchPanel } from "./WorkbenchPanel";
import type { WorkbenchExtensions } from "./workbench_extensions";
import { MockPort } from "../port/mock";
import type { ExtensionSurface } from "../port/wire";
import type { BrowserTab } from "../port/port";

const view = (pluginId: string, surfaceId: string, text: string): ExtensionSurface => ({
  pluginId, surfaceId, kind: "view", view: { body: [{ kind: "text", value: text }, { kind: "button", label: `run ${surfaceId}`, actionId: `run:${surfaceId}` }] },
});
const panel: ExtensionSurface = { pluginId: "sync", surfaceId: "state", kind: "panel", panel: { title: "Sync", text: "3 files behind" } };

function extensions(over: Partial<WorkbenchExtensions> = {}): WorkbenchExtensions {
  return { rail: [], panels: [], tabs: [], assigned: {}, onInvoke: vi.fn(), onMove: vi.fn(), ...over };
}

function Bench({ ext, shown = true, onSurfaces = vi.fn() }: { ext: WorkbenchExtensions; shown?: boolean; onSurfaces?: (n: number) => void }) {
  return <WorkbenchPanel scheme="dark" shown={shown} changes={[]} onSurfaces={onSurfaces} onCloseManual={vi.fn()} onExternal={vi.fn()} manual tabs={[]} port={new MockPort()} extensions={ext} />;
}

const tabs = () => [...document.querySelectorAll("[role=tab]")].map((e) => e.textContent?.trim());
const selected = () => document.querySelector("[role=tab][aria-selected=true]")?.textContent?.trim();
afterEach(cleanup);

describe("extension surfaces in the workbench", () => {
  it.each(["rail", "tabs"] as const)("closes a %s menu when its workbench is hidden without remounting the view", async (place) => {
    const ext = extensions({ [place]: [view("board", "kanban", "3 cards")] });
    const { rerender } = render(<Bench ext={ext} />);
    await screen.findByText("3 cards");
    const body = document.querySelector(".xview");
    await userEvent.click(screen.getByRole("button", { name: "移动 board 的界面" }));
    expect(screen.getByRole("menu")).toBeTruthy();
    rerender(<Bench ext={ext} shown={false} />);
    expect(screen.queryByRole("menu")).toBeNull();
    expect(document.querySelector(".xview")).toBe(body);
    rerender(<Bench ext={ext} />);
    expect(screen.queryByRole("menu")).toBeNull();
    expect(document.querySelector(".xview")).toBe(body);
    expect(ext.onMove).not.toHaveBeenCalled();
  });

  it("gives a view that asked for its own tab a tab named for its plugin, holding the view", async () => {
    const onSurfaces = vi.fn();
    render(<Bench ext={extensions({ tabs: [view("board", "kanban", "3 cards")] })} onSurfaces={onSurfaces} />);
    expect(tabs()).toEqual(["board"]);
    await waitFor(() => expect(onSurfaces).toHaveBeenLastCalledWith(1));
    expect(await screen.findByText("3 cards")).toBeTruthy();
  });

  it("puts the rail's views and panels together under one Extensions tab that cannot be closed", async () => {
    render(<Bench ext={extensions({ rail: [view("clock", "now", "12:30")], panels: [panel] })} />);
    expect(tabs()).toEqual(["扩展"]);
    expect(document.querySelector(".workbench-tab-close")).toBeNull();
    expect(await screen.findByText("12:30")).toBeTruthy();
    expect(screen.getByText("3 files behind")).toBeTruthy();
  });

  it("lists the rail after the tabs a view asked for, so a published view never takes the first place", () => {
    render(<Bench ext={extensions({ rail: [view("clock", "now", "12:30")], tabs: [view("board", "kanban", "3 cards")] })} />);
    expect(tabs()).toEqual(["board", "扩展"]);
  });

  it("shows no Extensions tab when the rail holds nothing", () => {
    render(<Bench ext={extensions({ tabs: [view("board", "kanban", "3 cards")] })} />);
    expect(tabs()).not.toContain("扩展");
  });

  it("closing a view's tab moves the view to the rail instead of losing it", async () => {
    const ext = extensions({ tabs: [view("board", "kanban", "3 cards")] });
    render(<Bench ext={ext} />);
    await userEvent.click(screen.getByRole("button", { name: "关闭 board" }));
    expect(ext.onMove).toHaveBeenCalledExactlyOnceWith(ext.tabs[0], "sidebar");
  });

  it("switches between extension tabs by the user's pick, and sends a button's action id unchanged", async () => {
    const ext = extensions({ rail: [view("clock", "now", "12:30")], tabs: [view("board", "kanban", "3 cards")] });
    render(<Bench ext={ext} />);
    expect(selected()).toBe("board");
    await userEvent.click(screen.getByRole("button", { name: "扩展" }));
    expect(selected()).toBe("扩展");
    await userEvent.click(await screen.findByRole("button", { name: "run now" }));
    expect(ext.onInvoke).toHaveBeenCalledExactlyOnceWith("run:now");
  });

  it("offers no browser tab menu on an extension tab", async () => {
    render(<Bench ext={extensions({ tabs: [view("board", "kanban", "3 cards")] })} />);
    const pick = screen.getByRole("button", { name: "board" });
    expect(pick.getAttribute("aria-haspopup")).toBeNull();
    await userEvent.pointer({ keys: "[MouseRight]", target: pick });
    expect(screen.queryByRole("menu", { name: /标签/ })).toBeNull();
  });
});

describe("extension tabs beside browser tabs", () => {
  const pages: BrowserTab[] = [
    { id: "a", target: "ta", title: "Alpha", url: "https://a.example", active: true },
    { id: "b", target: "tb", title: "Beta", url: "https://b.example", active: false },
  ];
  it("keeps extension tabs when a browser tab's menu closes the others", async () => {
    const ext = extensions({ rail: [view("clock", "now", "12:30")], tabs: [view("board", "kanban", "3 cards")] });
    render(<WorkbenchPanel scheme="dark" shown changes={[]} onSurfaces={vi.fn()} onCloseManual={vi.fn()} onExternal={vi.fn()} manual={false} tabs={pages} port={new MockPort()} extensions={ext} />);
    fireEvent.contextMenu(within(screen.getByRole("tab", { name: "Alpha" })).getByRole("button", { name: "Alpha" }), { clientX: 180, clientY: 50 });
    await userEvent.click(screen.getByRole("menuitem", { name: "关闭其他浏览器标签" }));
    await waitFor(() => expect(tabs()).toEqual(["Alpha", "board", "扩展"]));
    expect(ext.onMove).not.toHaveBeenCalled();
  });
});
