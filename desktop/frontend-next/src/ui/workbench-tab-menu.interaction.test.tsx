// @vitest-environment jsdom
import "./testkit";
import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { EditorView } from "@codemirror/view";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, it, vi } from "vitest";
import { MockPort } from "../port/mock";
import type { BrowserTab } from "../port/port";
import { WorkbenchPanel } from "./WorkbenchPanel";

afterEach(() => { cleanup(); vi.restoreAllMocks(); });
const pages: BrowserTab[] = [
  { id: "a", target: "ta", title: "Alpha", url: "https://a.example", active: true },
  { id: "b", target: "tb", title: "Beta", url: "https://b.example", active: false },
  { id: "c", target: "tc", title: "Gamma", url: "https://c.example", active: false },
];
function draw(tabs = pages, manual = false) {
  const port = new MockPort();
  const close = vi.spyOn(port, "browserClose");
  const props = { port, tabs, manual, shown: true, scheme: "light" as const, changes: [],
    onCloseManual: vi.fn(), onSurfaces: vi.fn(), onExternal: vi.fn() };
  const view = render(<WorkbenchPanel {...props} />);
  return { ...props, close, update: (next: BrowserTab[]) => view.rerender(<WorkbenchPanel {...props} tabs={next} />) };
}
const pick = (name: string) => within(screen.getByRole("tab", { name })).getByRole("button", { name });
const offer = (name: string) => fireEvent.contextMenu(pick(name), { clientX: 180, clientY: 50 });

it("offers a menu on an inactive browser tab without changing the selected page", async () => {
  const pane = draw();
  offer("Beta");
  expect(screen.getByRole("menu", { name: "浏览器标签" })).toBeTruthy();
  expect(screen.getByRole("tab", { name: "Alpha" }).getAttribute("aria-selected")).toBe("true");
  await userEvent.click(screen.getByRole("menuitem", { name: "关闭" }));
  expect(screen.queryByRole("tab", { name: "Beta" })).toBeNull();
  expect(screen.getByRole("tab", { name: "Alpha" }).getAttribute("aria-selected")).toBe("true");
  expect(pane.close).toHaveBeenCalledExactlyOnceWith("b");
  expect(pane.onCloseManual).not.toHaveBeenCalled();
  expect(pages).toHaveLength(3);
});

it("closes other browser tabs while retaining file tabs and the menu's browser", async () => {
  draw();
  await userEvent.click(await screen.findByRole("button", { name: "README.md" }));
  await userEvent.click(screen.getByRole("button", { name: "编辑" }));
  const editor = await waitFor(() => {
    const element = document.querySelector<HTMLElement>(".cm-content");
    if (!element) throw new Error("editor not mounted");
    return element;
  });
  const view = EditorView.findFromDOM(editor)!;
  act(() => view.dispatch({ changes: { from: view.state.doc.length, insert: "\n\n## Unsent edit" } }));
  offer("Beta");
  await userEvent.click(screen.getByRole("menuitem", { name: "关闭其他浏览器标签" }));
  expect(screen.queryByRole("tab", { name: "Alpha" })).toBeNull();
  expect(screen.queryByRole("tab", { name: "Gamma" })).toBeNull();
  expect(screen.getByRole("tab", { name: "Beta" })).toBeTruthy();
  expect(screen.getByRole("tab", { name: "README.md" }).getAttribute("aria-selected")).toBe("true");
  expect(document.activeElement).toBe(pick("Beta"));
  expect(document.querySelector(".cm-content")).toBe(editor);
  expect(view.state.doc.toString()).toContain("## Unsent edit");
});

it("closes only browser tabs to the right and keeps the current selection and files", async () => {
  const pane = draw();
  await userEvent.click(await screen.findByRole("button", { name: "README.md" }));
  await userEvent.click(pick("Alpha"));
  offer("Beta");
  await userEvent.click(screen.getByRole("menuitem", { name: "关闭右侧浏览器标签" }));
  expect(screen.queryByRole("tab", { name: "Gamma" })).toBeNull();
  expect(screen.getByRole("tab", { name: "Alpha" }).getAttribute("aria-selected")).toBe("true");
  expect(screen.getByRole("tab", { name: "Beta" })).toBeTruthy();
  expect(screen.getByRole("tab", { name: "README.md" })).toBeTruthy();
  expect(pane.close).toHaveBeenCalledExactlyOnceWith("c");
});

it("retains a refused right-side page and disables close controls while awaiting the controller", async () => {
  let refuse: ((error: Error) => void) | undefined;
  const pane = draw();
  pane.close.mockImplementation(() => new Promise<void>((_resolve, reject) => { refuse = reject; }));
  offer("Beta");
  await userEvent.click(screen.getByRole("menuitem", { name: "关闭右侧浏览器标签" }));
  expect(pane.close).toHaveBeenCalledExactlyOnceWith("c");
  expect(screen.getByRole("tab", { name: "Gamma" })).toBeTruthy();
  expect((screen.getByRole("button", { name: "关闭 Gamma" }) as HTMLButtonElement).disabled).toBe(true);
  offer("Beta");
  expect(screen.queryByRole("menu")).toBeNull();
  await act(async () => refuse?.(new Error("controller refused close")));
  expect(screen.getByRole("tab", { name: "Gamma" })).toBeTruthy();
  expect(screen.getByRole("alert").textContent).toContain("controller refused close");
  expect((screen.getByRole("button", { name: "关闭 Gamma" }) as HTMLButtonElement).disabled).toBe(false);
});

it("disables empty bulk choices and folds the column with its last browser", async () => {
  const pane = draw([pages[0]!]);
  offer("Alpha");
  expect((screen.getByRole("menuitem", { name: "关闭其他浏览器标签" }) as HTMLButtonElement).disabled).toBe(true);
  expect((screen.getByRole("menuitem", { name: "关闭右侧浏览器标签" }) as HTMLButtonElement).disabled).toBe(true);
  await userEvent.click(screen.getByRole("menuitem", { name: "关闭" }));
  expect(pane.onCloseManual).toHaveBeenCalledTimes(1);
  expect(screen.queryByRole("menu")).toBeNull();
});

it("supports keyboard opening and menu navigation, then focuses the remaining tab", async () => {
  draw();
  await userEvent.click(pick("Beta"));
  await userEvent.keyboard("{Shift>}{F10}{/Shift}");
  expect(document.activeElement).toBe(screen.getByRole("menuitem", { name: "关闭" }));
  await userEvent.keyboard("{ArrowDown}");
  expect(document.activeElement).toBe(screen.getByRole("menuitem", { name: "关闭其他浏览器标签" }));
  await userEvent.keyboard("{Home}{Enter}");
  expect(screen.queryByRole("tab", { name: "Beta" })).toBeNull();
  expect(document.activeElement).toBe(pick("Alpha"));
});

it("cancels with Escape without dismissing a tab and restores the trigger's focus", async () => {
  const pane = draw();
  offer("Beta");
  await userEvent.keyboard("{Escape}");
  expect(screen.queryByRole("menu")).toBeNull();
  expect(document.activeElement).toBe(pick("Beta"));
  expect(screen.getAllByRole("tab")).toHaveLength(3);
  expect(pane.onCloseManual).not.toHaveBeenCalled();
});

it("drops a menu whose browser disappears and leaves file context menus native", async () => {
  const pane = draw();
  offer("Beta");
  pane.update([pages[0]!, pages[2]!]);
  expect(screen.queryByRole("menu")).toBeNull();
  await userEvent.click(await screen.findByRole("button", { name: "README.md" }));
  expect(fireEvent.contextMenu(pick("README.md"))).toBe(true);
  expect(screen.queryByRole("menu")).toBeNull();
});

it("offers the same actions on manual browser tabs", async () => {
  const pane = draw([], true);
  await userEvent.click(screen.getByRole("button", { name: "新建浏览器标签" }));
  await userEvent.click(screen.getByRole("button", { name: "新建浏览器标签" }));
  const tabs = screen.getAllByRole("tab", { name: "浏览器" });
  fireEvent.contextMenu(within(tabs[1]!).getByRole("button", { name: "浏览器" }));
  await userEvent.click(screen.getByRole("menuitem", { name: "关闭其他浏览器标签" }));
  expect(screen.getAllByRole("tab", { name: "浏览器" })).toHaveLength(1);
  expect(pane.onCloseManual).not.toHaveBeenCalled();
  await waitFor(() => expect(pane.onSurfaces).toHaveBeenLastCalledWith(1));
});

it("leaves the native menu available when the tab title has selected text", () => {
  draw();
  const title = pick("Beta").querySelector("span")!;
  const range = document.createRange();
  range.selectNodeContents(title);
  const selection = window.getSelection()!;
  selection.removeAllRanges();
  selection.addRange(range);
  try {
    expect(offer("Beta")).toBe(true);
    expect(screen.queryByRole("menu")).toBeNull();
  } finally { selection.removeAllRanges(); }
});

it("dismisses the menu on an outside press and skips disabled menu items by keyboard", async () => {
  draw([pages[0]!]);
  offer("Alpha");
  await userEvent.keyboard("{End}");
  expect(document.activeElement).toBe(screen.getByRole("menuitem", { name: "关闭全部浏览器标签" }));
  await userEvent.keyboard("{ArrowUp}");
  expect(document.activeElement).toBe(screen.getByRole("menuitem", { name: "关闭" }));
  fireEvent.pointerDown(document.body);
  expect(screen.queryByRole("menu")).toBeNull();
  expect(document.activeElement).toBe(pick("Alpha"));
  expect(screen.getByRole("tab", { name: "Alpha" })).toBeTruthy();
});
