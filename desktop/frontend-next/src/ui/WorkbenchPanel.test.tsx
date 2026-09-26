// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { EditorView } from "@codemirror/view";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { MockPort } from "../port/mock";
import { HttpError } from "../port/port";
import { WorkbenchPanel } from "./WorkbenchPanel";

afterEach(cleanup);

describe("WorkbenchPanel", () => {
  it("opens workspace files as tabs and saves what was typed in the editor", async () => {
    const user = userEvent.setup();
    const port = new MockPort();
    const save = vi.spyOn(port, "saveWorkspaceFile");
    render(<WorkbenchPanel port={port} tabs={[]} manual={false} shown scheme="light" changes={[]} onCloseManual={vi.fn()} onSurfaces={vi.fn()} onExternal={vi.fn()} />);

    await user.click(await screen.findByRole("button", { name: "README.md" }));
    expect(screen.getByRole("tab", { name: "README.md" }).getAttribute("aria-selected")).toBe("true");
    // A document opens rendered; editing is the next mode over.
    await waitFor(() => expect(document.querySelector(".workbench-read .md")).toBeTruthy());
    expect(screen.getByRole("button", { name: "阅读" }).getAttribute("aria-pressed")).toBe("true");
    await user.click(screen.getByRole("button", { name: "编辑" }));
    const content = await waitFor(() => {
      const el = document.querySelector(".cm-content");
      if (!el) throw new Error("editor not mounted");
      return el as HTMLElement;
    });
    const view = EditorView.findFromDOM(content)!;
    act(() => view.dispatch({ changes: { from: view.state.doc.length, insert: "updated" } }));
    await user.click(screen.getByRole("button", { name: "保存" }));
    await waitFor(() => expect(save).toHaveBeenCalled());
    expect(save.mock.calls[0][0].content.endsWith("updated")).toBe(true);
    // What reading shows is the draft, so an edit is visible before it is saved.
    act(() => view.dispatch({ changes: { from: view.state.doc.length, insert: "\n\n## Draft heading" } }));
    await user.click(screen.getByRole("button", { name: "阅读" }));
    await waitFor(() => expect(document.querySelector(".workbench-read h2")?.textContent).toBe("Draft heading"));
  });

  it("shows the start page only while the agent has none, and gives way to the agent's", () => {
    const props = { port: new MockPort(), manual: true, shown: false, scheme: "dark" as const, changes: [], onCloseManual: vi.fn(), onSurfaces: vi.fn(), onExternal: vi.fn() };
    const { rerender } = render(<WorkbenchPanel {...props} tabs={[]} />);
    expect(screen.getByRole("tab", { name: "浏览器" })).toBeTruthy();
    rerender(<WorkbenchPanel {...props} tabs={[{ id: "b1", target: "t1", url: "https://top.baidu.com", title: "百度热搜", active: true }]} />);
    expect(screen.queryByRole("tab", { name: "浏览器" })).toBeNull();
    expect(screen.getByRole("tab", { name: "百度热搜" }).getAttribute("aria-selected")).toBe("true");
  });

  it("keeps the column while another tab is open, and folds it with the last", async () => {
    const user = userEvent.setup();
    const onCloseManual = vi.fn();
    const pages = [
      { id: "b1", target: "t1", url: "https://top.baidu.com", title: "Agent page", active: true },
      { id: "b2", target: "t2", url: "https://example.com", title: "My page", active: false },
    ];
    render(<WorkbenchPanel port={new MockPort()} tabs={pages} manual shown={false} scheme="dark" changes={[]} onCloseManual={onCloseManual} onSurfaces={vi.fn()} onExternal={vi.fn()} />);
    await user.click(screen.getByRole("button", { name: "关闭 Agent page" }));
    expect(onCloseManual).not.toHaveBeenCalled();
    expect(screen.getByRole("tab", { name: "My page" })).toBeTruthy();
    await user.click(screen.getByRole("button", { name: "关闭 My page" }));
    expect(onCloseManual).toHaveBeenCalledTimes(1);
  });

  it("shows a page the person opened beside the agent's, while the agent's tab stays active", () => {
    const props = { port: new MockPort(), manual: true, shown: false, scheme: "dark" as const, changes: [], onCloseManual: vi.fn(), onSurfaces: vi.fn(), onExternal: vi.fn() };
    const agent = { id: "b1", target: "t1", url: "https://top.baidu.com", title: "百度热搜", active: true };
    const { rerender } = render(<WorkbenchPanel {...props} tabs={[agent]} />);
    rerender(<WorkbenchPanel {...props} tabs={[agent, { id: "b2", target: "t2", url: "https://example.com", title: "Link", active: false }]} />);
    expect(screen.getByRole("tab", { name: "Link" }).getAttribute("aria-selected")).toBe("true");
  });

  it("shows the file that was picked from the list rather than leaving the list over it", async () => {
    const user = userEvent.setup();
    const { container } = render(<WorkbenchPanel port={new MockPort()} tabs={[]} manual={false} shown scheme="light" changes={[]} onCloseManual={vi.fn()} onSurfaces={vi.fn()} onExternal={vi.fn()} />);
    await user.click(screen.getByRole("button", { name: "文件" }));
    expect(container.querySelector(".workbench-body")?.hasAttribute("data-files")).toBe(true);
    await user.click(await screen.findByRole("button", { name: "README.md" }));
    expect(container.querySelector(".workbench-body")?.hasAttribute("data-files")).toBe(false);
    expect(screen.getByRole("tab", { name: "README.md" }).getAttribute("aria-selected")).toBe("true");
  });

  // The port answers from a disk the test owns, and the delete goes around every
  // port call — the way a file manager does it — so nothing announces it.
  it("drops a folder deleted outside the app once the window is looked at again", async () => {
    const disk = new Map<string, "dir" | "file">([["t620", "dir"], ["kept", "dir"], ["skills-lock.json", "file"]]);
    const port = new MockPort();
    vi.spyOn(port, "workspaceFiles").mockImplementation(async () => ({
      files: [...disk].filter(([, kind]) => kind === "file").map(([name]) => name),
      directories: [...disk].filter(([, kind]) => kind === "dir").map(([name]) => name),
    }));
    render(<WorkbenchPanel port={port} tabs={[]} manual={false} shown scheme="light" changes={[]} onCloseManual={vi.fn()} onSurfaces={vi.fn()} onExternal={vi.fn()} />);
    await screen.findByRole("button", { name: "t620" });

    disk.delete("t620");
    act(() => {
      window.dispatchEvent(new Event("focus"));
    });

    await waitFor(() => expect(screen.queryByRole("button", { name: "t620" })).toBeNull());
    expect(screen.getByRole("button", { name: "kept" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "skills-lock.json" })).toBeTruthy();
  });

  it("reads the tree again when a turn settles, whatever git lists as changed", async () => {
    const disk = new Set(["notes.md"]);
    const port = new MockPort();
    vi.spyOn(port, "workspaceFiles").mockImplementation(async () => ({ files: [...disk], directories: [] }));
    const props = { port, tabs: [], manual: false, shown: true, scheme: "light" as const, changes: [], onCloseManual: vi.fn(), onSurfaces: vi.fn(), onExternal: vi.fn() };
    const { rerender } = render(<WorkbenchPanel {...props} running />);
    await screen.findByRole("button", { name: "notes.md" });

    disk.add("report.html");
    rerender(<WorkbenchPanel {...props} running={false} />);

    expect(await screen.findByRole("button", { name: "report.html" })).toBeTruthy();
  });

  it("shows the workspace and any entry in the system file manager through the port", async () => {
    const user = userEvent.setup();
    const port = new MockPort();
    const reveal = vi.spyOn(port, "revealInFileManager");
    render(<WorkbenchPanel port={port} tabs={[]} manual={false} shown scheme="light" changes={[]} onCloseManual={vi.fn()} onSurfaces={vi.fn()} onExternal={vi.fn()} />);

    await user.click(screen.getByRole("button", { name: "在系统文件管理器中显示工作区" }));
    expect(reveal).toHaveBeenLastCalledWith("");

    const folder = await screen.findByRole("button", { name: "internal" });
    fireEvent.contextMenu(folder);
    await user.click(screen.getByRole("menuitem", { name: "在系统文件管理器中显示" }));
    expect(reveal).toHaveBeenLastCalledWith("internal");
    expect(screen.queryByRole("menu")).toBeNull();

    // From the keyboard: the menu key reports no pointer, and the item takes focus.
    fireEvent.contextMenu(screen.getByRole("button", { name: "README.md" }), { clientX: 0, clientY: 0 });
    expect(document.activeElement).toBe(screen.getByRole("menuitem", { name: "在系统文件管理器中显示" }));
    await user.keyboard("{Enter}");
    expect(reveal).toHaveBeenLastCalledWith("README.md");
  });

  it("offers no way to reveal a remote workspace's paths on this machine", async () => {
    const port = new MockPort();
    const reveal = vi.spyOn(port, "revealInFileManager");
    render(<WorkbenchPanel port={port} tabs={[]} manual={false} shown scheme="light" changes={[]} remote onCloseManual={vi.fn()} onSurfaces={vi.fn()} onExternal={vi.fn()} />);
    const row = await screen.findByRole("button", { name: "README.md" });

    expect(screen.queryByRole("button", { name: "在系统文件管理器中显示工作区" })).toBeNull();
    // Not prevented: the system menu is left to the shell.
    expect(fireEvent.contextMenu(row)).toBe(true);
    expect(screen.queryByRole("menu")).toBeNull();
    expect(reveal).not.toHaveBeenCalled();
  });

  it("says why a reveal was refused, in the explorer", async () => {
    const user = userEvent.setup();
    const port = new MockPort();
    vi.spyOn(port, "revealInFileManager").mockRejectedValue(
      new HttpError(403, "no window", { code: "workspace.locate_no_window", error: "no window" }),
    );
    render(<WorkbenchPanel port={port} tabs={[]} manual={false} shown scheme="light" changes={[]} onCloseManual={vi.fn()} onSurfaces={vi.fn()} onExternal={vi.fn()} />);
    await user.click(screen.getByRole("button", { name: "在系统文件管理器中显示工作区" }));
    expect((await screen.findByRole("alert")).textContent).toBe("这个内核不在本机，没法在系统文件管理器中显示它的文件。");
  });

  const diskPort = (disk: { content: string; revision: string }) => {
    const port = new MockPort();
    vi.spyOn(port, "workspaceFiles").mockImplementation(async () => ({ files: ["notes.md"], directories: [] }));
    vi.spyOn(port, "workspaceFile").mockImplementation(async (path: string) => ({ path, ...disk }));
    return port;
  };

  it("shows an open file as it is on disk once a turn settles, without reopening it", async () => {
    const user = userEvent.setup();
    const disk = { content: "# First", revision: "r1" };
    const props = { port: diskPort(disk), tabs: [], manual: false, shown: true, scheme: "light" as const, changes: [], onCloseManual: vi.fn(), onSurfaces: vi.fn(), onExternal: vi.fn() };
    const { rerender } = render(<WorkbenchPanel {...props} running />);
    await user.click(await screen.findByRole("button", { name: "notes.md" }));
    await waitFor(() => expect(document.querySelector(".workbench-read h1")?.textContent).toBe("First"));

    Object.assign(disk, { content: "# Second", revision: "r2" });
    rerender(<WorkbenchPanel {...props} running={false} />);

    await waitFor(() => expect(document.querySelector(".workbench-read h1")?.textContent).toBe("Second"));
  });

  it("shows an open file as it is on disk once the window is looked at again", async () => {
    const user = userEvent.setup();
    const disk = { content: "# First", revision: "r1" };
    render(<WorkbenchPanel port={diskPort(disk)} tabs={[]} manual={false} shown scheme="light" changes={[]} onCloseManual={vi.fn()} onSurfaces={vi.fn()} onExternal={vi.fn()} />);
    await user.click(await screen.findByRole("button", { name: "notes.md" }));
    await waitFor(() => expect(document.querySelector(".workbench-read h1")?.textContent).toBe("First"));

    Object.assign(disk, { content: "# Second", revision: "r2" });
    act(() => {
      window.dispatchEvent(new Event("focus"));
    });

    await waitFor(() => expect(document.querySelector(".workbench-read h1")?.textContent).toBe("Second"));
  });

  it("keeps unsaved edits when the file changes on disk underneath them", async () => {
    const user = userEvent.setup();
    const disk = { content: "# First", revision: "r1" };
    const port = diskPort(disk);
    const props = { port, tabs: [], manual: false, shown: true, scheme: "light" as const, changes: [], onCloseManual: vi.fn(), onSurfaces: vi.fn(), onExternal: vi.fn() };
    const { rerender } = render(<WorkbenchPanel {...props} running />);
    await user.click(await screen.findByRole("button", { name: "notes.md" }));
    await waitFor(() => expect(document.querySelector(".workbench-read h1")?.textContent).toBe("First"));
    await user.click(screen.getByRole("button", { name: "编辑" }));
    const content = await waitFor(() => {
      const el = document.querySelector(".cm-content");
      if (!el) throw new Error("editor not mounted");
      return el as HTMLElement;
    });
    const view = EditorView.findFromDOM(content)!;
    act(() => view.dispatch({ changes: { from: view.state.doc.length, insert: "\n\n## Mine" } }));
    const reads = vi.mocked(port.workspaceFile).mock.calls.length;

    Object.assign(disk, { content: "# Second", revision: "r2" });
    rerender(<WorkbenchPanel {...props} running={false} />);
    await waitFor(() => expect(vi.mocked(port.workspaceFiles).mock.calls.length).toBeGreaterThan(1));

    expect(vi.mocked(port.workspaceFile).mock.calls.length).toBe(reads);
    expect(view.state.doc.toString()).toBe("# First\n\n## Mine");
  });
  // Opens notes.md in the editor, then holds the next disk read until the test
  // releases it, so an edit or a save can land while that read is in flight.
  const openWithHeldRead = async () => {
    const user = userEvent.setup();
    const disk = { content: "# First", revision: "r1" };
    const port = diskPort(disk);
    const props = { port, tabs: [], manual: false, shown: true, scheme: "light" as const, changes: [], onCloseManual: vi.fn(), onSurfaces: vi.fn(), onExternal: vi.fn() };
    const { rerender } = render(<WorkbenchPanel {...props} running />);
    await user.click(await screen.findByRole("button", { name: "notes.md" }));
    await waitFor(() => expect(document.querySelector(".workbench-read h1")?.textContent).toBe("First"));
    await user.click(screen.getByRole("button", { name: "编辑" }));
    const content = await waitFor(() => {
      const el = document.querySelector(".cm-content");
      if (!el) throw new Error("editor not mounted");
      return el as HTMLElement;
    });
    const view = EditorView.findFromDOM(content)!;
    let release: (file: { path: string; content: string; revision: string }) => void = () => {};
    vi.mocked(port.workspaceFile).mockImplementationOnce(() => new Promise((done) => (release = done)));
    const reads = vi.mocked(port.workspaceFile).mock.calls.length;
    rerender(<WorkbenchPanel {...props} running={false} />);
    await waitFor(() => expect(vi.mocked(port.workspaceFile).mock.calls.length).toBe(reads + 1));
    const land = async (file: { content: string; revision: string }) => {
      await act(async () => {
        release({ path: "notes.md", ...file });
        await Promise.resolve();
      });
    };
    return { user, port, view, land };
  };

  it("keeps an edit made while a reload was reading the file", async () => {
    const { view, land } = await openWithHeldRead();

    act(() => view.dispatch({ changes: { from: view.state.doc.length, insert: "\n\n## Mine" } }));
    await land({ content: "# Second", revision: "r2" });

    expect(view.state.doc.toString()).toBe("# First\n\n## Mine");
  });

  it("drops a reload that was issued before a save and lands after it", async () => {
    const { user, port, view, land } = await openWithHeldRead();
    vi.spyOn(port, "saveWorkspaceFile").mockImplementation(async (file) => ({ ...file, revision: "r3" }));

    act(() => view.dispatch({ changes: { from: view.state.doc.length, insert: "\n\n## Saved" } }));
    await user.click(screen.getByRole("button", { name: "保存" }));
    await waitFor(() => expect(port.saveWorkspaceFile).toHaveBeenCalled());
    await land({ content: "# Second", revision: "r2" });

    expect(view.state.doc.toString()).toBe("# First\n\n## Saved");
  });
});
