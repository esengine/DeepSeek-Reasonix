// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { EditorView } from "@codemirror/view";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, it, vi } from "vitest";
import type { WorkspaceFile } from "../port/port";
import { MockPort } from "../port/mock";
import { WorkbenchPanel } from "./WorkbenchPanel";
import "./testkit";

Range.prototype.getClientRects = () => [] as unknown as DOMRectList;
Range.prototype.getBoundingClientRect = () => new DOMRect(0, 0, 0, 0);
afterEach(cleanup);

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (error: Error) => void;
  const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no; });
  return { promise, resolve, reject };
}
const file = (path: string, content = `saved ${path}`, revision = "r1") => ({ path, content, revision });
const button = (action: string, target?: string) => document.querySelector<HTMLButtonElement>(
  `[data-action="workbench.${action}"]${target ? `[data-target="${target}"]` : ""}, [data-action-click="workbench.${action}"]${target ? `[data-target="${target}"]` : ""}`,
)!;
async function editor(value?: string) {
  return waitFor(() => {
    const element = document.querySelector(".cm-content");
    if (!element) throw new Error("editor not mounted");
    const view = EditorView.findFromDOM(element as HTMLElement)!;
    if (value !== undefined) expect(view.state.doc.toString()).toBe(value);
    return view;
  });
}
async function append(text: string) {
  const view = await editor();
  act(() => view.dispatch({ changes: { from: view.state.doc.length, insert: text } }));
}
function fixture() {
  const user = userEvent.setup();
  const port = new MockPort();
  vi.spyOn(port, "workspaceFiles").mockResolvedValue({ files: ["a.txt", "b.txt"], directories: [] });
  const read = vi.spyOn(port, "workspaceFile").mockImplementation(async (path) => file(path));
  const save = vi.spyOn(port, "saveWorkspaceFile").mockImplementation(async (file) => ({ ...file, revision: "r2" }));
  const props = { port, tabs: [], manual: false, shown: true, scheme: "light" as const, changes: [], workspaceRoot: "/one", onCloseManual: vi.fn(), onSurfaces: vi.fn(), onExternal: vi.fn() };
  const view = render(<WorkbenchPanel {...props} />);
  // Saving is the shortcut now; the file bar carries no button to click.
  const ctrlS = () => fireEvent.keyDown(window, { key: "s", ctrlKey: true });
  const open = async (path: string) => {
    await user.click(button("files"));
    await user.click(await screen.findByRole("button", { name: path }));
  };
  return { ...view, user, port, read, save, ctrlS, props, open };
}

it("preserves both dirty file tabs without saving or reloading either draft", async () => {
  const f = fixture();
  await f.open("a.txt");
  await editor("saved a.txt");
  await append(" UNSAVED USER WORK");
  await f.open("b.txt");
  await editor("saved b.txt");
  await append(" B draft");
  await f.user.click(button("tab", "file:a.txt"));
  await editor("saved a.txt UNSAVED USER WORK");
  await f.user.click(button("tab", "file:b.txt"));
  await editor("saved b.txt B draft");
  expect(f.save).not.toHaveBeenCalled();
  expect(f.read).toHaveBeenCalledTimes(2);
});

it("keeps a draft through launcher cancellation and hiding, and a close asks before dropping it", async () => {
  const f = fixture();
  await f.open("a.txt");
  await append(" draft");
  await f.user.click(button("new"));
  await f.user.keyboard("{Escape}");
  await editor("saved a.txt draft");
  f.rerender(<WorkbenchPanel {...f.props} shown={false} />);
  f.rerender(<WorkbenchPanel {...f.props} />);
  await editor("saved a.txt draft");
  // The close is a question now; staying is one of its answers.
  await f.user.click(button("close", "file:a.txt"));
  await f.user.click(await screen.findByRole("button", { name: "取消" }));
  await editor("saved a.txt draft");
  expect(f.save).not.toHaveBeenCalled();
});

it.each([false, true])("keeps a late save result on its own file (failure: %s)", async (fails) => {
  const f = fixture();
  const pending = deferred<WorkspaceFile>();
  f.save.mockReturnValueOnce(pending.promise);
  await f.open("a.txt");
  await append(" draft");
  f.ctrlS();
  await f.open("b.txt");
  await editor("saved b.txt");
  await append(" other");
  await act(async () => {
    if (fails) pending.reject(new Error("save refused"));
    else {
      f.read.mockImplementation(async (path) => path === "a.txt" ? file(path, "saved a.txt draft", "r2") : file(path));
      pending.resolve(file("a.txt", "saved a.txt draft", "r2"));
    }
  });
  await editor("saved b.txt other");
  expect(screen.queryByText("save refused")).toBeNull();
  await f.user.click(button("tab", "file:a.txt"));
  await editor("saved a.txt draft");
  if (!fails) expect(screen.queryByRole("alert")).toBeNull();
  if (fails) expect(screen.getByRole("alert").textContent).toBe("save refused");
  expect(f.save.mock.calls[0][0]).toEqual(file("a.txt", "saved a.txt draft"));
});

it("retains edits typed during saving and suppresses repeated save shortcuts", async () => {
  const f = fixture();
  const pending = deferred<WorkspaceFile>();
  f.save.mockReturnValueOnce(pending.promise);
  await f.open("a.txt");
  await append(" draft");
  f.ctrlS();
  await append(" newer");
  (await editor()).focus();
  await f.user.keyboard("{Control>}s{/Control}");
  expect(f.save).toHaveBeenCalledTimes(1);
  await act(async () => pending.resolve(file("a.txt", "saved a.txt draft", "r2")));
  await editor("saved a.txt draft newer");
  f.ctrlS();
  expect(f.save.mock.calls[1][0]).toEqual(file("a.txt", "saved a.txt draft newer", "r2"));
});

it.each(["workspace", "connection"])("isolates same-path drafts and pending saves after a %s change", async (change) => {
  const f = fixture();
  const pending = deferred<WorkspaceFile>();
  f.save.mockReturnValueOnce(pending.promise);
  await f.open("a.txt");
  await append(" old draft");
  f.ctrlS();
  const nextPort = change === "connection" ? new MockPort() : f.port;
  const nextRead = deferred<WorkspaceFile>();
  vi.spyOn(nextPort, "workspaceFile").mockReturnValue(nextRead.promise);
  const nextSave = vi.spyOn(nextPort, "saveWorkspaceFile").mockImplementation(async (file) => ({ ...file, revision: "next-saved" }));
  nextSave.mockClear();
  f.rerender(<WorkbenchPanel {...f.props} port={nextPort} workspaceRoot={change === "workspace" ? "/two" : "/one"} />);
  expect(document.querySelector(".cm-content")).toBeNull();
  await act(async () => nextRead.resolve(file("a.txt", "new workspace", "next")));
  await editor("new workspace");
  await append(" draft");
  await act(async () => pending.resolve(file("a.txt", "saved a.txt old draft", "r2")));
  await editor("new workspace draft");
  f.ctrlS();
  expect(nextSave).toHaveBeenCalledExactlyOnceWith(file("a.txt", "new workspace draft", "next"));
});

it.each(["workspace", "connection"])("ignores an old %s read and refreshes the same path in the new scope", async (change) => {
  const f = fixture();
  const old = deferred<WorkspaceFile>();
  f.read.mockReturnValueOnce(old.promise);
  await f.open("a.txt");
  const nextPort = change === "connection" ? new MockPort() : f.port;
  vi.spyOn(nextPort, "workspaceFile").mockResolvedValue(file("a.txt", "new scope", "next"));
  f.rerender(<WorkbenchPanel {...f.props} port={nextPort} workspaceRoot={change === "workspace" ? "/two" : "/one"} />);
  await editor("new scope");
  await act(async () => old.resolve(file("a.txt", "old scope")));
  await editor("new scope");
  expect(f.save).not.toHaveBeenCalled();
});

it("does not save the previous draft while another file is loading and ignores out-of-order reads", async () => {
  const f = fixture();
  await f.open("a.txt");
  await append(" draft");
  const first = deferred<WorkspaceFile>();
  const second = deferred<WorkspaceFile>();
  f.read.mockReturnValueOnce(first.promise).mockReturnValueOnce(second.promise);
  await f.open("b.txt");
  f.ctrlS();
  expect(f.save).not.toHaveBeenCalled();
  await f.user.click(button("tab", "file:a.txt"));
  await editor("saved a.txt draft");
  await f.user.click(button("tab", "file:b.txt"));
  await act(async () => second.resolve(file("b.txt", "latest read", "r3")));
  await editor("latest read");
  await act(async () => first.resolve(file("b.txt", "stale read", "r2")));
  await editor("latest read");
});

it("refreshes clean tabs and their diffs while keeping another tab's dirty draft", async () => {
  const f = fixture();
  await f.open("a.txt");
  await append(" draft");
  await f.open("b.txt");
  await editor("saved b.txt");
  f.read.mockImplementation(async (path) => file(path, `changed ${path}`, "r2"));
  vi.spyOn(f.port, "changeDiff").mockResolvedValue({ path: "b.txt", diff: "diff --git a/b.txt b/b.txt\n--- a/b.txt\n+++ b/b.txt\n@@ -1 +1 @@\n-old\n+new\n", truncated: false });
  f.rerender(<WorkbenchPanel {...f.props} wrote={1} />);
  await editor("changed b.txt");
  await f.user.click(document.querySelector('[data-action="workbench.mode"][data-value="diff"]')!);
  await waitFor(() => expect(document.querySelector(".workbench-diff")?.textContent).toContain("new"));
  await f.user.click(button("tab", "file:a.txt"));
  await f.user.click(document.querySelector('[data-action="workbench.mode"][data-value="file"]')!);
  await editor("saved a.txt draft");
});

it("resumes Markdown polling after changing modes during a pending poll", async () => {
  vi.useFakeTimers({ toFake: ["setInterval", "clearInterval"] });
  try {
    const f = fixture();
    vi.mocked(f.port.workspaceFiles).mockResolvedValue({ files: ["notes.md"], directories: [] });
    f.rerender(<WorkbenchPanel {...f.props} wrote={1} />);
    f.read.mockResolvedValue(file("notes.md", "# First"));
    await f.open("notes.md");
    await waitFor(() => expect(document.querySelector(".workbench-read h1")?.textContent).toBe("First"));
    const pending = deferred<WorkspaceFile>();
    f.read.mockReturnValueOnce(pending.promise);
    await act(async () => vi.advanceTimersByTimeAsync(6000));
    await f.user.click(document.querySelector('[data-action="workbench.mode"][data-value="file"]')!);
    await f.user.click(document.querySelector('[data-action="workbench.mode"][data-value="read"]')!);
    f.read.mockResolvedValue(file("notes.md", "# Latest", "r3"));
    await act(async () => vi.advanceTimersByTimeAsync(3000));
    await waitFor(() => expect(document.querySelector(".workbench-read h1")?.textContent).toBe("Latest"));
    await act(async () => pending.resolve(file("notes.md", "# Stale", "r2")));
    expect(document.querySelector(".workbench-read h1")?.textContent).toBe("Latest");
  } finally { vi.useRealTimers(); }
});

it("evicts a clean closed file and asks before a dirty one is dropped", async () => {
  const f = fixture();
  await f.open("a.txt");
  await editor("saved a.txt");
  // Clean: no question, and the closed entry is evicted, so reopening reads.
  await f.user.click(button("close", "file:a.txt"));
  expect(screen.queryByRole("dialog")).toBeNull();
  const cleanRead = deferred<WorkspaceFile>();
  f.read.mockReturnValueOnce(cleanRead.promise);
  await f.open("a.txt");
  expect(document.querySelector(".cm-content")).toBeNull();
  await act(async () => cleanRead.resolve(file("a.txt")));
  await editor("saved a.txt");
  // Dirty: the question stands between the draft and the disk.
  await append(" draft");
  await f.user.click(button("close", "file:a.txt"));
  await f.user.click(await screen.findByRole("button", { name: "取消" }));
  await editor("saved a.txt draft");
  // Saving clears the mark, so the next close needs no answer.
  f.ctrlS();
  await waitFor(() => expect(f.save).toHaveBeenCalledTimes(1));
  await f.user.click(button("close", "file:a.txt"));
  expect(screen.queryByRole("dialog")).toBeNull();
  const savedRead = deferred<WorkspaceFile>();
  f.read.mockReturnValueOnce(savedRead.promise);
  await f.open("a.txt");
  expect(document.querySelector(".cm-content")).toBeNull();
  await act(async () => savedRead.resolve(file("a.txt", "saved a.txt draft", "r2")));
  await editor("saved a.txt draft");
});

it.each(["read", "save"])("ignores a late %s after unmounting and remounting the same workspace", async (operation) => {
  const f = fixture();
  const pending = deferred<WorkspaceFile>();
  if (operation === "read") f.read.mockReturnValueOnce(pending.promise);
  else f.save.mockReturnValueOnce(pending.promise);
  await f.open("a.txt");
  if (operation === "save") {
    await append(" old draft");
    f.ctrlS();
  }
  f.unmount();
  f.read.mockResolvedValue(file("a.txt", "remounted", "r3"));
  render(<WorkbenchPanel {...f.props} />);
  await f.open("a.txt");
  await editor("remounted");
  await append(" new draft");
  await act(async () => pending.resolve(file("a.txt", "old response", "r2")));
  await editor("remounted new draft");
  expect(f.save).toHaveBeenCalledTimes(operation === "save" ? 1 : 0);
});
