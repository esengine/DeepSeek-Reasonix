// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { EditorView } from "@codemirror/view";
import { openSearchPanel } from "@codemirror/search";
import { afterEach, expect, it, vi } from "vitest";
import { host } from "../port/host";
import { MockPort } from "../port/mock";
import { WorkbenchPanel } from "./WorkbenchPanel";
import "./testkit";

Range.prototype.getClientRects = () => [] as unknown as DOMRectList;
const clipboard = Object.getOwnPropertyDescriptor(navigator, "clipboard");
afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  document.getSelection()?.removeAllRanges();
  if (clipboard) Object.defineProperty(navigator, "clipboard", clipboard);
  else Reflect.deleteProperty(navigator, "clipboard");
});

function setup(path = "notes.txt", readOnly = false) {
  const port = new MockPort();
  vi.spyOn(port, "workspaceFiles").mockResolvedValue({ files: [path, "other.txt"], directories: [] });
  vi.spyOn(port, "workspaceFile").mockImplementation(async (file) => ({
    path: file, content: file === "guide.md" ? "# Guide\n\nRun the installer, then restart." : "  alpha\nbeta  ", revision: "r1", readOnly,
  }));
  const onQuoteSelection = vi.fn();
  const props = {
    port, workspaceRoot: "/work/project", tabs: [], manual: false, shown: true, scheme: "light" as const,
    changes: [], onCloseManual: vi.fn(), onSurfaces: vi.fn(), onExternal: vi.fn(), onQuoteSelection,
    fileRequest: { path },
  };
  return { ...render(<WorkbenchPanel {...props} />), props, port, onQuoteSelection };
}

async function editor() {
  return waitFor(() => {
    const content = document.querySelector<HTMLElement>(".cm-content");
    if (!content) throw new Error("editor not mounted");
    const view = EditorView.findFromDOM(content);
    if (!view) throw new Error("editor unavailable");
    return view;
  });
}

function mark(view: EditorView, from = 0, to = view.state.doc.length) {
  act(() => { view.dispatch({ selection: { anchor: from, head: to } }); view.focus(); });
}

function point(view: EditorView) {
  return fireEvent.contextMenu(view.contentDOM, { clientX: 30, clientY: 40 });
}

async function reading() {
  return waitFor(() => {
    const read = document.querySelector<HTMLElement>(".workbench-read");
    if (!read?.querySelector("p")) throw new Error("reader not mounted");
    return read;
  });
}

function select(read: HTMLElement, text: string) {
  const node = read.querySelector("p")!.firstChild!;
  const range = document.createRange();
  const from = node.textContent!.indexOf(text);
  range.setStart(node, from);
  range.setEnd(node, from + text.length);
  const selection = document.getSelection()!;
  selection.removeAllRanges();
  selection.addRange(range);
  return node.parentElement!;
}

const names = () => within(screen.getByRole("menu")).getAllByRole("menuitem").map((el) => el.textContent);

it("integrates editing actions and quotes the exact CodeMirror selection, including whitespace and newlines", async () => {
  const user = userEvent.setup();
  const { onQuoteSelection } = setup();
  const view = await editor();
  mark(view);
  expect(point(view)).toBe(false);
  await screen.findByRole("menuitem", { name: "加入对话" });
  expect(names()).toEqual(["撤销编辑", "重做", "剪切", "复制", "粘贴", "全选", "加入对话"]);
  await user.click(screen.getByRole("menuitem", { name: "加入对话" }));
  expect(onQuoteSelection).toHaveBeenCalledExactlyOnceWith("  alpha\nbeta  ", "notes.txt");
  expect(document.activeElement).toBe(view.contentDOM);
});

it("treats editable Markdown as an editor, not as a read-only file extension", async () => {
  const user = userEvent.setup();
  setup("guide.md");
  await reading();
  await user.click(screen.getByRole("button", { name: "编辑" }));
  const view = await editor();
  mark(view, 0, 7);
  point(view);
  expect(await screen.findByRole("menuitem", { name: "剪切" })).toBeTruthy();
  expect(screen.getByRole("menuitem", { name: "粘贴" })).toBeTruthy();
});

it("keeps ordinary editing available without a selection and disables cut/copy without offering a quote", async () => {
  setup();
  const view = await editor();
  mark(view, 3, 3);
  point(view);
  await screen.findByRole("menuitem", { name: "全选" });
  expect(names()).toEqual(["撤销编辑", "重做", "剪切", "复制", "粘贴", "全选"]);
  expect((screen.getByRole("menuitem", { name: "剪切" }) as HTMLButtonElement).disabled).toBe(true);
  expect((screen.getByRole("menuitem", { name: "复制" }) as HTMLButtonElement).disabled).toBe(true);
  expect((screen.getByRole("menuitem", { name: "撤销编辑" }) as HTMLButtonElement).disabled).toBe(true);
  expect((screen.getByRole("menuitem", { name: "重做" }) as HTMLButtonElement).disabled).toBe(true);
});

it("copies whitespace selections but only offers quoting for nonblank text", async () => {
  const user = userEvent.setup();
  const writeText = vi.fn().mockResolvedValue(undefined);
  Object.defineProperty(navigator, "clipboard", { configurable: true, value: { writeText } });
  setup();
  const view = await editor();
  mark(view, 0, 2);
  point(view);
  await user.click(await screen.findByRole("menuitem", { name: "复制" }));
  expect(writeText).toHaveBeenCalledWith("  ");
  expect(screen.queryByRole("menuitem", { name: "加入对话" })).toBeNull();
});

it("uses the CodeMirror history for cut, undo, redo, paste and select all", async () => {
  const user = userEvent.setup();
  const writeText = vi.fn().mockResolvedValue(undefined);
  const readText = vi.fn().mockResolvedValue("INSERT");
  Object.defineProperty(navigator, "clipboard", { configurable: true, value: { writeText, readText } });
  setup();
  const view = await editor();
  mark(view, 2, 7);
  point(view);
  await user.click(await screen.findByRole("menuitem", { name: "剪切" }));
  await waitFor(() => expect(view.state.doc.toString()).toBe("  \nbeta  "));
  expect(writeText).toHaveBeenCalledWith("alpha");
  point(view);
  await user.click(await screen.findByRole("menuitem", { name: "撤销编辑" }));
  expect(view.state.doc.toString()).toBe("  alpha\nbeta  ");
  point(view);
  await user.click(await screen.findByRole("menuitem", { name: "重做" }));
  expect(view.state.doc.toString()).toBe("  \nbeta  ");
  point(view);
  await user.click(await screen.findByRole("menuitem", { name: "粘贴" }));
  await waitFor(() => expect(view.state.doc.toString()).toBe("  INSERT\nbeta  "));
  point(view);
  await user.click(await screen.findByRole("menuitem", { name: "全选" }));
  expect(view.state.selection.main.from).toBe(0);
  expect(view.state.selection.main.to).toBe(view.state.doc.length);
});

it("surfaces paste permission failures without modifying the document", async () => {
  const user = userEvent.setup();
  Object.defineProperty(navigator, "clipboard", { configurable: true, value: { readText: vi.fn().mockRejectedValue(new Error("Clipboard permission denied")) } });
  setup();
  const view = await editor();
  mark(view, 2, 7);
  point(view);
  await user.click(await screen.findByRole("menuitem", { name: "粘贴" }));
  expect(await screen.findByRole("alert")).toHaveProperty("textContent", expect.stringContaining("Clipboard permission denied"));
  expect(view.state.doc.toString()).toBe("  alpha\nbeta  ");
});

it("offers reader copy/select-all/quote only, preserving exact selected text and its snapshot", async () => {
  const user = userEvent.setup();
  const writeText = vi.fn().mockResolvedValue(undefined);
  Object.defineProperty(navigator, "clipboard", { configurable: true, value: { writeText } });
  const { onQuoteSelection } = setup("guide.md");
  const read = await reading();
  fireEvent.contextMenu(select(read, " installer, then "), { clientX: 20, clientY: 20 });
  await screen.findByRole("menuitem", { name: "复制" });
  expect(names()).toEqual(["复制", "全选", "加入对话"]);
  await user.click(screen.getByRole("menuitem", { name: "复制" }));
  expect(writeText).toHaveBeenCalledWith(" installer, then ");
  expect(document.getSelection()!.toString()).toBe(" installer, then ");
  fireEvent.contextMenu(select(read, " installer, then "));
  await screen.findByRole("menuitem", { name: "加入对话" });
  select(read, "restart");
  await user.click(screen.getByRole("menuitem", { name: "加入对话" }));
  expect(onQuoteSelection).toHaveBeenCalledExactlyOnceWith(" installer, then ", "guide.md");
});

it("selects all within the reader, not the rest of the application", async () => {
  const user = userEvent.setup();
  setup("guide.md");
  const read = await reading();
  fireEvent.contextMenu(select(read, "restart"));
  await user.click(await screen.findByRole("menuitem", { name: "全选" }));
  const selection = document.getSelection()!;
  expect(read.contains(selection.anchorNode)).toBe(true);
  expect(read.contains(selection.focusNode)).toBe(true);
  expect(selection.toString()).toContain("Run the installer");
  expect(selection.toString()).not.toContain("工作台");
});

it("uses the actual read-only CodeMirror state and leaves empty readers alone", async () => {
  setup("notes.txt", true);
  const view = await editor();
  expect(point(view)).toBe(true);
  expect(screen.queryByRole("menu")).toBeNull();
  mark(view, 2, 7);
  point(view);
  await screen.findByRole("menuitem", { name: "复制" });
  expect(names()).toEqual(["复制", "全选", "加入对话"]);
});

it("requires both reader selection endpoints inside the surface and ignores controls/search", async () => {
  setup("guide.md");
  const read = await reading();
  const target = select(read, "restart");
  const selection = document.getSelection()!;
  const range = selection.getRangeAt(0);
  range.setStart(document.querySelector(".workbench-filebar strong")!.firstChild!, 0);
  selection.removeAllRanges();
  selection.addRange(range);
  expect(fireEvent.contextMenu(target)).toBe(true);
  expect(screen.queryByRole("menu")).toBeNull();
  expect(fireEvent.contextMenu(screen.getByRole("button", { name: "编辑" }))).toBe(true);
  selection.removeAllRanges();
});

it("does not intercept the editor's search panel", async () => {
  setup();
  const view = await editor();
  act(() => { openSearchPanel(view); });
  const input = document.querySelector<HTMLInputElement>(".cm-search input")!;
  expect(input).toBeTruthy();
  expect(fireEvent.contextMenu(input)).toBe(true);
  expect(fireEvent.keyDown(input, { key: "ContextMenu" })).toBe(true);
  expect(screen.queryByRole("menu")).toBeNull();
});

it.each([
  { key: "ContextMenu", readOnly: false }, { key: "F10", shiftKey: true, readOnly: false },
  { key: "ContextMenu", readOnly: true }, { key: "F10", shiftKey: true, readOnly: true },
])("supports keyboard context menus and restores editor focus/selection on Escape (%j)", async ({ readOnly, ...key }) => {
  const user = userEvent.setup();
  const { onQuoteSelection } = setup("notes.txt", readOnly);
  const view = await editor();
  mark(view, 2, 7);
  fireEvent.keyDown(view.contentDOM, key);
  await screen.findByRole("menuitem", { name: "加入对话" });
  await user.keyboard("{Escape}");
  expect(screen.queryByRole("menu")).toBeNull();
  expect(document.activeElement).toBe(view.contentDOM);
  expect(view.state.sliceDoc(view.state.selection.main.from, view.state.selection.main.to)).toBe("alpha");
  expect(onQuoteSelection).not.toHaveBeenCalled();
});

it("supports reader keyboard invocation, navigation and dismissal without submitting", async () => {
  const user = userEvent.setup();
  const { onQuoteSelection } = setup("guide.md");
  const read = await reading();
  read.focus();
  select(read, "restart");
  expect(fireEvent.keyDown(read, { key: "F10", shiftKey: true })).toBe(false);
  const copy = await screen.findByRole("menuitem", { name: "复制" });
  await waitFor(() => expect(document.activeElement).toBe(copy));
  await user.keyboard("{End}");
  expect(document.activeElement).toBe(screen.getByRole("menuitem", { name: "加入对话" }));
  await user.keyboard("{Escape}");
  expect(document.activeElement).toBe(read);
  expect(document.getSelection()!.toString()).toBe("restart");
  expect(onQuoteSelection).not.toHaveBeenCalled();
});

it("invalidates browser menus when switching modes or files", async () => {
  const user = userEvent.setup();
  const { rerender, props, onQuoteSelection } = setup("guide.md");
  const read = await reading();
  fireEvent.contextMenu(select(read, "restart"));
  await screen.findByRole("menuitem", { name: "加入对话" });
  await user.click(screen.getByRole("button", { name: "编辑" }));
  expect(screen.queryByRole("menu")).toBeNull();
  const view = await editor();
  mark(view);
  point(view);
  await screen.findByRole("menuitem", { name: "加入对话" });
  rerender(<WorkbenchPanel {...props} fileRequest={{ path: "other.txt" }} />);
  await waitFor(() => expect(document.querySelector(".workbench-filebar strong")!.textContent).toBe("other.txt"));
  expect(screen.queryByRole("menu")).toBeNull();
  expect(onQuoteSelection).not.toHaveBeenCalled();
});

it("prevents the default menu and routes native quote/dismissal without rendering a second menu", async () => {
  const native = vi.spyOn(host(), "showTextContextMenu").mockResolvedValue("quote");
  const { onQuoteSelection } = setup();
  const view = await editor();
  mark(view);
  expect(point(view)).toBe(false);
  await waitFor(() => expect(onQuoteSelection).toHaveBeenCalledExactlyOnceWith("  alpha\nbeta  ", "notes.txt"));
  expect(native).toHaveBeenCalledWith({ editable: true, selected: true, canUndo: false, canRedo: false, quote: true, x: 30, y: 40 });
  expect(screen.queryByRole("menu")).toBeNull();
  native.mockResolvedValue("");
  mark(view, 2, 7);
  point(view);
  await waitFor(() => expect(native).toHaveBeenCalledTimes(2));
  expect(onQuoteSelection).toHaveBeenCalledTimes(1);
  expect(document.activeElement).toBe(view.contentDOM);
  expect(screen.queryByRole("menu")).toBeNull();
});

it("ignores delayed native results after file replacement or a newer menu request", async () => {
  const results: Array<(result: "quote" | "") => void> = [];
  vi.spyOn(host(), "showTextContextMenu").mockImplementation(() => new Promise((resolve) => results.push(resolve)));
  const { rerender, props, onQuoteSelection } = setup();
  const view = await editor();
  mark(view, 2, 7);
  point(view);
  mark(view, 8, 12);
  point(view);
  await act(async () => { results[0]!("quote"); });
  expect(onQuoteSelection).not.toHaveBeenCalled();
  rerender(<WorkbenchPanel {...props} fileRequest={{ path: "other.txt" }} />);
  await waitFor(() => expect(document.querySelector(".workbench-filebar strong")!.textContent).toBe("other.txt"));
  await act(async () => { results[1]!("quote"); });
  expect(onQuoteSelection).not.toHaveBeenCalled();
});

it("does not apply a pending clipboard mutation to a replacement editor", async () => {
  const user = userEvent.setup();
  let resolve!: (text: string) => void;
  Object.defineProperty(navigator, "clipboard", { configurable: true, value: { readText: vi.fn(() => new Promise<string>((done) => { resolve = done; })) } });
  const { rerender, props } = setup();
  const view = await editor();
  mark(view, 2, 7);
  point(view);
  await user.click(await screen.findByRole("menuitem", { name: "粘贴" }));
  rerender(<WorkbenchPanel {...props} fileRequest={{ path: "other.txt" }} />);
  await waitFor(() => expect(document.querySelector(".workbench-filebar strong")!.textContent).toBe("other.txt"));
  await act(async () => { resolve("wrong file"); });
  expect((await editor()).state.doc.toString()).toBe("  alpha\nbeta  ");
});

it("routes native undo/redo/select-all results through the originating CodeMirror history", async () => {
  const native = vi.spyOn(host(), "showTextContextMenu").mockResolvedValue("undo");
  setup();
  const view = await editor();
  act(() => { view.dispatch({ changes: { from: 2, to: 7, insert: "changed" }, userEvent: "input.type" }); });
  point(view);
  await waitFor(() => expect(view.state.doc.toString()).toBe("  alpha\nbeta  "));
  expect(native).toHaveBeenLastCalledWith(expect.objectContaining({ editable: true, canUndo: true, canRedo: false }));
  native.mockResolvedValue("redo");
  point(view);
  await waitFor(() => expect(view.state.doc.toString()).toBe("  changed\nbeta  "));
  expect(native).toHaveBeenLastCalledWith(expect.objectContaining({ canUndo: false, canRedo: true }));
  native.mockResolvedValue("selectAll");
  point(view);
  await waitFor(() => expect(view.state.selection.main.to).toBe(view.state.doc.length));
  expect(view.state.selection.main.from).toBe(0);
  expect(screen.queryByRole("menu")).toBeNull();
});

it("routes native reader select-all to the reader surface, not the application", async () => {
  vi.spyOn(host(), "showTextContextMenu").mockResolvedValue("selectAll");
  setup("guide.md");
  const read = await reading();
  fireEvent.contextMenu(select(read, "restart"));
  await waitFor(() => expect(document.getSelection()!.toString()).toContain("Run the installer"));
  expect(read.contains(document.getSelection()!.anchorNode)).toBe(true);
  expect(read.contains(document.getSelection()!.focusNode)).toBe(true);
});

it("invalidates a delayed native quote when changing reader/source mode", async () => {
  const user = userEvent.setup();
  let resolve!: (action: "quote") => void;
  // This package targets ES2022, which has no Promise.withResolvers.
  vi.spyOn(host(), "showTextContextMenu").mockImplementation(() => new Promise((done) => { resolve = done; }));
  const { onQuoteSelection } = setup("guide.md");
  const read = await reading();
  fireEvent.contextMenu(select(read, "restart"));
  await user.click(screen.getByRole("button", { name: "编辑" }));
  await editor();
  await act(async () => { resolve("quote"); });
  expect(onQuoteSelection).not.toHaveBeenCalled();
  expect(screen.queryByRole("menu")).toBeNull();
});

it("does not show a browser fallback when a native popup fails", async () => {
  vi.spyOn(host(), "showTextContextMenu").mockRejectedValue(new Error("Native popup refused"));
  setup();
  const view = await editor();
  mark(view);
  point(view);
  expect(await screen.findByRole("alert")).toHaveProperty("textContent", expect.stringContaining("Native popup refused"));
  expect(screen.queryByRole("menu")).toBeNull();
  expect(document.activeElement).toBe(view.contentDOM);
});
