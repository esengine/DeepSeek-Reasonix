// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { EditorView } from "@codemirror/view";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { MockPort } from "../port/mock";
import { HttpError } from "../port/port";
import * as hostModule from "../port/host";
import { SsePort } from "../port/sse";
import { WorkbenchPanel } from "./WorkbenchPanel";
import { unseenPageOpenings } from "./browser_open";

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
    fireEvent.keyDown(window, { key: "s", ctrlKey: true });
    await waitFor(() => expect(save).toHaveBeenCalled());
    expect(save.mock.calls[0][0].content.endsWith("updated")).toBe(true);
    // What reading shows is the draft, so an edit is visible before it is saved.
    act(() => view.dispatch({ changes: { from: view.state.doc.length, insert: "\n\n## Draft heading" } }));
    await user.click(screen.getByRole("button", { name: "阅读" }));
    await waitFor(() => expect(document.querySelector(".workbench-read h2")?.textContent).toBe("Draft heading"));
  });

  it("updates an open Markdown document after it changes on disk", async () => {
    vi.useFakeTimers({ toFake: ["setInterval", "clearInterval"] });
    try {
      const user = userEvent.setup();
      const port = new MockPort();
      let content = "# First version";
      const read = vi.spyOn(port, "workspaceFile").mockImplementation(async (path) => ({ path, content, revision: "fixture" }));
      render(<WorkbenchPanel port={port} tabs={[]} manual={false} shown scheme="light" changes={[]} onCloseManual={vi.fn()} onSurfaces={vi.fn()} onExternal={vi.fn()} />);

      await user.click(await screen.findByRole("button", { name: "README.md" }));
      await waitFor(() => expect(document.querySelector(".workbench-read h1")?.textContent).toBe("First version"));
      content = "# Updated on disk";
      await act(async () => { await vi.advanceTimersByTimeAsync(3000); });
      expect(document.querySelector(".workbench-read h1")?.textContent).toBe("Updated on disk");
      expect(read).toHaveBeenCalledTimes(2);

      read.mockRejectedValueOnce(new Error("atomic save in progress"));
      await act(async () => { await vi.advanceTimersByTimeAsync(3000); });
      expect(document.querySelector(".workbench-read h1")?.textContent).toBe("Updated on disk");
      expect(screen.queryByText("atomic save in progress")).toBeNull();

      await user.click(screen.getByRole("button", { name: "编辑" }));
      const editor = await waitFor(() => {
        const element = document.querySelector(".cm-content");
        if (!element) throw new Error("editor not mounted");
        return EditorView.findFromDOM(element as HTMLElement)!;
      });
      act(() => editor.dispatch({ changes: { from: editor.state.doc.length, insert: "\n\n## Local draft" } }));
      await user.click(screen.getByRole("button", { name: "阅读" }));
      content = "# Changed again on disk";
      await act(async () => { await vi.advanceTimersByTimeAsync(3000); });
      expect(document.querySelector(".workbench-read h2")?.textContent).toBe("Local draft");
      expect(read).toHaveBeenCalledTimes(3);
    } finally {
      vi.useRealTimers();
    }
  });

  describe("a browser that cannot open", () => {
    const drawsViews = () => {
      vi.spyOn(hostModule.host(), "drawsBrowserViews").mockReturnValue(true);
    };
    afterEach(() => {
      vi.restoreAllMocks();
      vi.unstubAllGlobals();
    });

    const KERNEL_WORDS: [string, string, string][] = [
      ["browser.engine_missing", "no Chrome, Edge or Chromium is installed", "没有找到可用的浏览器。请安装 Chrome、Edge 或 Chromium，或在配置里用 [browser] executable 指定路径，新会话才会读到"],
      ["browser.engine_failed", "start chrome: exec failed", "内置浏览器没能启动，稍后再试一次"],
      ["browser.profile_busy", "another browser is already running", "内置浏览器的资料目录正被另一个浏览器占用。关掉其他 Studio 窗口或用同一资料目录的浏览器后再试"],
      ["browser.open_failed", "this session has no browser", "打不开这个网页：this session has no browser"],
    ];
    const refusedLikeTheKernel = (port: MockPort, code: string, detail: string) => {
      vi.stubGlobal("fetch", async () => new Response(JSON.stringify({ code, error: detail, params: { url: "about:blank", error: detail } }), { status: 400 }));
      const wire = new SsePort("", "r1");
      vi.spyOn(port, "browserOpen").mockImplementation((url: string) => wire.browserOpen(url, true));
    };

    for (const [code, detail, words] of KERNEL_WORDS) {
      it(`says why when the browser is chosen on the launcher and the kernel refuses with ${code}`, async () => {
        drawsViews();
        const user = userEvent.setup();
        const port = new MockPort();
        refusedLikeTheKernel(port, code, detail);
        render(<WorkbenchPanel port={port} tabs={[]} manual shown scheme="light" changes={[]} onCloseManual={vi.fn()} onSurfaces={vi.fn()} onExternal={vi.fn()} />);
        expect(screen.queryByRole("alert")).toBeNull();
        await user.click(screen.getByRole("button", { name: "浏览器" }));
        const alert = await screen.findByRole("alert");
        expect(alert.textContent).toBe(words);
        expect(alert.textContent).not.toContain("{");
        expect(unseenPageOpenings(port)).toHaveLength(0);
      });

      it(`says why when the browser chosen after + is refused with ${code}`, async () => {
        drawsViews();
        const user = userEvent.setup();
        const port = new MockPort();
        refusedLikeTheKernel(port, code, detail);
        render(<WorkbenchPanel port={port} tabs={[]} manual={false} shown scheme="light" changes={[]} onCloseManual={vi.fn()} onSurfaces={vi.fn()} onExternal={vi.fn()} />);
        await user.click(screen.getByRole("button", { name: "新建标签" }));
        await user.click(screen.getByRole("button", { name: "浏览器" }));
        const alert = await screen.findByRole("alert");
        expect(alert.textContent).toBe(words);
        expect(alert.textContent).not.toContain("{");
        expect(unseenPageOpenings(port)).toHaveLength(0);
      });
    }

    it("clears the reason once a file is picked", async () => {
      drawsViews();
      const user = userEvent.setup();
      const port = new MockPort();
      vi.spyOn(port, "browserOpen").mockRejectedValue(new Error("profile is in use"));
      render(<WorkbenchPanel port={port} tabs={[]} manual={false} shown scheme="light" changes={[]} onCloseManual={vi.fn()} onSurfaces={vi.fn()} onExternal={vi.fn()} />);
      await user.click(screen.getByRole("button", { name: "新建标签" }));
      await user.click(screen.getByRole("button", { name: "浏览器" }));
      await screen.findByRole("alert");
      await user.click(await screen.findByRole("button", { name: "README.md" }));
      await waitFor(() => expect(screen.queryByRole("alert")).toBeNull());
    });

    it("clears the reason when a later open succeeds", async () => {
      drawsViews();
      const user = userEvent.setup();
      const port = new MockPort();
      const open = vi.spyOn(port, "browserOpen");
      open.mockRejectedValueOnce(new Error("profile is in use"));
      render(<WorkbenchPanel port={port} tabs={[]} manual={false} shown scheme="light" changes={[]} onCloseManual={vi.fn()} onSurfaces={vi.fn()} onExternal={vi.fn()} />);
      await user.click(screen.getByRole("button", { name: "新建标签" }));
      await user.click(screen.getByRole("button", { name: "浏览器" }));
      await screen.findByRole("alert");
      await user.click(screen.getByRole("button", { name: "浏览器" }));
      await waitFor(() => expect(screen.queryByRole("alert")).toBeNull());
      expect(open).toHaveBeenCalledTimes(2);
    });

    it("keeps a file's own read error on the file tab and drops it with the tab", async () => {
      const user = userEvent.setup();
      const port = new MockPort();
      vi.spyOn(port, "workspaceFile").mockRejectedValue(new Error("file is gone"));
      render(<WorkbenchPanel port={port} tabs={[]} manual={false} shown scheme="light" changes={[]} onCloseManual={vi.fn()} onSurfaces={vi.fn()} onExternal={vi.fn()} />);
      await user.click(await screen.findByRole("button", { name: "README.md" }));
      expect((await screen.findAllByRole("alert")).length).toBe(1);
      await user.click(screen.getByRole("button", { name: "关闭 README.md" }));
      await waitFor(() => expect(screen.queryByRole("alert")).toBeNull());
    });
  });

  it("offers a new-tab choice without replacing the current file or its draft", async () => {
    const user = userEvent.setup();
    const port = new MockPort();
    const open = vi.spyOn(port, "browserOpen");
    const { container } = render(<WorkbenchPanel port={port} tabs={[]} manual shown scheme="light" changes={[]} onCloseManual={vi.fn()} onSurfaces={vi.fn()} onExternal={vi.fn()} />);
    await user.click(await screen.findByRole("button", { name: "README.md" }));
    await user.click(screen.getByRole("button", { name: "编辑" }));
    const editor = await waitFor(() => {
      const element = container.querySelector(".cm-content");
      if (!element) throw new Error("editor not mounted");
      return EditorView.findFromDOM(element as HTMLElement)!;
    });
    act(() => editor.dispatch({ changes: { from: editor.state.doc.length, insert: "local draft" } }));
    const plus = container.querySelector<HTMLButtonElement>(".workbench-new")!;
    await user.click(plus);
    expect(screen.getByRole("group", { name: "开始" })).toBeTruthy();
    expect(document.activeElement?.getAttribute("data-action")).toBe("workbench.launch-files");
    expect(screen.getAllByRole("tab")).toHaveLength(1);
    expect(open).not.toHaveBeenCalled();
    expect((screen.getByRole("button", { name: /^改动/ }) as HTMLButtonElement).disabled).toBe(true);
    await user.keyboard("{Escape}");
    expect(screen.queryByRole("group", { name: "开始" })).toBeNull();
    expect(document.activeElement).toBe(plus);
    expect(EditorView.findFromDOM(container.querySelector(".cm-content")!)).toBe(editor);
    expect(editor.state.doc.toString()).toContain("local draft");
    await user.click(plus);
    await user.click(plus);
    expect(screen.queryByRole("group", { name: "开始" })).toBeNull();
    expect(screen.getAllByRole("tab")).toHaveLength(1);
    expect(open).not.toHaveBeenCalled();
    await user.click(plus);
    await user.click(screen.getByRole("button", { name: "浏览器" }));
    expect(screen.getAllByRole("tab")).toHaveLength(2);
    await user.click(screen.getByRole("tab", { name: "README.md" }).querySelector("button")!);
    expect(editor.state.doc.toString()).toContain("local draft");
  });

  it("leaves new-tab choice through Files, Changes or an existing browser", async () => {
    const user = userEvent.setup();
    const port = new MockPort();
    const open = vi.spyOn(port, "browserOpen");
    const page = { id: "b1", target: "t1", url: "https://example.com", title: "Existing page", active: true };
    const { container } = render(<WorkbenchPanel port={port} tabs={[page]} manual shown scheme="light" changes={[{ path: "README.md", status: "M" }]} onCloseManual={vi.fn()} onSurfaces={vi.fn()} onExternal={vi.fn()} />);
    const plus = container.querySelector<HTMLButtonElement>(".workbench-new")!;
    await user.click(plus);
    await user.click(container.querySelector('[data-action="workbench.launch-files"]')!);
    expect(container.querySelector(".workbench-body")?.hasAttribute("data-files")).toBe(true);
    await user.click(plus);
    await user.click(container.querySelector('[data-action="workbench.launch-changes"]')!);
    expect(screen.getByRole("tab", { name: "README.md" }).getAttribute("aria-selected")).toBe("true");
    await user.click(plus);
    await user.click(screen.getByRole("tab", { name: "Existing page" }).querySelector("button")!);
    expect(screen.queryByRole("group", { name: "开始" })).toBeNull();
    expect(screen.getAllByRole("tab")).toHaveLength(2);
    expect(open).not.toHaveBeenCalled();
  });

  it("opens one native browser only after an explicit choice, including repeated clicks", async () => {
    const user = userEvent.setup();
    const port = new MockPort();
    const views = vi.spyOn(hostModule.host(), "drawsBrowserViews").mockReturnValue(true);
    let finish!: (value: Awaited<ReturnType<MockPort["browserOpen"]>>) => void;
    const open = vi.spyOn(port, "browserOpen").mockImplementation(() => new Promise((resolve) => { finish = resolve; }));
    try {
      render(<WorkbenchPanel port={port} tabs={[]} manual shown scheme="light" changes={[]} onCloseManual={vi.fn()} onSurfaces={vi.fn()} onExternal={vi.fn()} />);
      await user.click(screen.getByRole("button", { name: "新建标签" }));
      expect(open).not.toHaveBeenCalled();
      await user.dblClick(screen.getByRole("button", { name: "浏览器" }));
      expect(open).toHaveBeenCalledTimes(1);
      await act(async () => finish({ id: "b1", target: "t1", title: "", url: "about:blank", active: false }));
    } finally { views.mockRestore(); }
  });

  it("hides native browser views while choosing and keeps the launcher when its last tab closes", async () => {
    const user = userEvent.setup();
    const views = vi.spyOn(hostModule.host(), "drawsBrowserViews").mockReturnValue(true);
    const hide = vi.spyOn(hostModule.host(), "hideBrowserView");
    vi.stubGlobal("ResizeObserver", class { observe() {} disconnect() {} });
    const close = vi.fn();
    try {
      render(<WorkbenchPanel port={new MockPort()} tabs={[{ id: "b1", target: "t1", url: "about:blank", title: "Existing page", active: true }]} manual shown scheme="light" changes={[]} onCloseManual={close} onSurfaces={vi.fn()} onExternal={vi.fn()} />);
      hide.mockClear();
      await user.click(screen.getByRole("button", { name: "新建标签" }));
      expect(hide).toHaveBeenCalled();
      await user.click(screen.getByRole("button", { name: "关闭 Existing page" }));
      expect(close).not.toHaveBeenCalled();
      expect(screen.getByRole("group", { name: "开始" })).toBeTruthy();
    } finally { views.mockRestore(); hide.mockRestore(); vi.unstubAllGlobals(); }
  });

  it.each(["escape", "plus"])("restores the explorer when new-tab choice is cancelled with %s", async (cancel) => {
    const user = userEvent.setup();
    const { container } = render(<WorkbenchPanel port={new MockPort()} tabs={[]} manual shown scheme="light" changes={[]} onCloseManual={vi.fn()} onSurfaces={vi.fn()} onExternal={vi.fn()} />);
    await user.click(container.querySelector('[data-action="workbench.files"]')!);
    await user.click(screen.getByRole("button", { name: "新建标签" }));
    expect(container.querySelector(".workbench-body")?.hasAttribute("data-files")).toBe(false);
    if (cancel === "escape") await user.keyboard("{Escape}");
    else await user.click(screen.getByRole("button", { name: "新建标签" }));
    expect(container.querySelector(".workbench-body")?.hasAttribute("data-files")).toBe(true);
  });

  it.each([false, true])("respects newer Files choices (reopen: %s)", async (reopen) => {
    const user = userEvent.setup();
    const port = new MockPort();
    const views = vi.spyOn(hostModule.host(), "drawsBrowserViews").mockReturnValue(true);
    let finish!: (value: Awaited<ReturnType<MockPort["browserOpen"]>>) => void;
    vi.spyOn(port, "browserOpen").mockImplementation(() => new Promise((resolve) => { finish = resolve; }));
    try {
      const props = { port, tabs: [], manual: true, shown: true, scheme: "light" as const, changes: [], onCloseManual: vi.fn(), onSurfaces: vi.fn(), onExternal: vi.fn() };
      const { container } = render(<WorkbenchPanel {...props} />);
      await user.click(screen.getByRole("button", { name: "新建标签" }));
      await user.click(screen.getByRole("button", { name: "浏览器" }));
      await user.click(container.querySelector('[data-action="workbench.launch-files"]')!);
      if (reopen) {
        await user.click(screen.getByRole("button", { name: "新建标签" }));
        await user.click(screen.getByRole("button", { name: "浏览器" }));
      }
      await act(async () => finish({ id: "b1", target: "t1", title: "", url: "about:blank", active: false }));
      expect(container.querySelector(".workbench-body")?.hasAttribute("data-files")).toBe(!reopen);
      expect(screen.getByRole("button", { name: "新建标签" }).getAttribute("aria-expanded")).toBe("false");
      expect(port.browserOpen).toHaveBeenCalledTimes(1);
    } finally { views.mockRestore(); }
  });

  it.each([
    ["event-first", false, false], ["reply-first", false, false], ["event-first", true, false], ["reply-first", true, false],
    ["event-first", false, true], ["reply-first", false, true], ["event-first", true, true], ["reply-first", true, true],
  ])("filters superseded native tab events (%s, mixed agent batch: %s, rejected: %s)", async (order, mixed, rejected) => {
    const user = userEvent.setup();
    const port = new MockPort();
    const views = vi.spyOn(hostModule.host(), "drawsBrowserViews").mockReturnValue(true);
    let finish!: (value: Awaited<ReturnType<MockPort["browserOpen"]>>) => void;
    let fail!: (error: unknown) => void;
    vi.spyOn(port, "browserOpen").mockImplementation(() => new Promise((resolve, reject) => { finish = resolve; fail = reject; }));
    const agent = { id: "a", target: "ta", url: "https://example.com", title: "Agent page", active: true };
    const opened = { id: "b", target: "tb", url: "about:blank", title: "New browser", active: false };
    const settle = () => rejected ? fail(new HttpError(400, "failed", { code: "browser.open_failed", params: { tab: opened.id } })) : finish(opened);
    const props = { port, manual: true, shown: false, scheme: "light" as const, changes: [], onCloseManual: vi.fn(), onSurfaces: vi.fn(), onExternal: vi.fn() };
    try {
      const { container, rerender } = render(<WorkbenchPanel {...props} tabs={[agent]} />);
      await user.click(screen.getByRole("button", { name: "新建标签" }));
      await user.click(screen.getByRole("button", { name: "浏览器" }));
      await user.click(container.querySelector('[data-action="workbench.launch-files"]')!);
      if (order === "reply-first") await act(async () => settle());
      const fresh = { ...agent, id: "c", target: "tc", title: "New agent page", active: false };
      rerender(<WorkbenchPanel {...props} tabs={mixed ? [agent, fresh, opened] : [agent, opened]} />);
      if (order === "event-first") {
        expect(container.querySelector(".workbench-body")?.hasAttribute("data-files")).toBe(true);
        await act(async () => settle());
      }
      await waitFor(() => expect(container.querySelector(".workbench-body")?.hasAttribute("data-files")).toBe(!mixed));
      expect(unseenPageOpenings(port)).toHaveLength(0);
      if (!mixed) rerender(<WorkbenchPanel {...props} tabs={[agent, opened, fresh]} />);
      await waitFor(() => expect(screen.getByRole("tab", { name: "New agent page" }).getAttribute("aria-selected")).toBe("true"));
      rerender(<WorkbenchPanel {...props} tabs={[{ ...agent, active: false }, { ...opened, active: true, url: "https://reused.example" }, fresh]} />);
      await waitFor(() => expect(screen.getByRole("tab", { name: "New browser" }).getAttribute("aria-selected")).toBe("true"));
    } finally { views.mockRestore(); }
  });

  it("still follows a newly discovered agent page while the launcher is open", async () => {
    const user = userEvent.setup();
    const props = { port: new MockPort(), manual: true, shown: false, scheme: "light" as const, changes: [], onCloseManual: vi.fn(), onSurfaces: vi.fn(), onExternal: vi.fn() };
    const { rerender } = render(<WorkbenchPanel {...props} tabs={[]} />);
    await user.click(screen.getByRole("button", { name: "新建标签" }));
    rerender(<WorkbenchPanel {...props} tabs={[{ id: "b1", target: "t1", url: "https://example.com", title: "Agent page", active: true }]} />);
    expect(screen.queryByRole("group", { name: "开始" })).toBeNull();
    expect(screen.getByRole("tab", { name: "Agent page" }).getAttribute("aria-selected")).toBe("true");
  });

  it("opens on a launcher that offers a choice, and no browser until one is chosen", async () => {
    const user = userEvent.setup();
    const props = { port: new MockPort(), manual: true, shown: false, scheme: "dark" as const, changes: [], onCloseManual: vi.fn(), onSurfaces: vi.fn(), onExternal: vi.fn() };
    render(<WorkbenchPanel {...props} tabs={[]} />);
    expect(screen.getByRole("group", { name: "开始" })).toBeTruthy();
    expect(screen.queryByRole("tab", { name: "浏览器" })).toBeNull();
    expect((screen.getByRole("button", { name: /^改动/ }) as HTMLButtonElement).disabled).toBe(true);
    await user.click(screen.getByRole("button", { name: "浏览器" }));
    expect(screen.getByRole("tab", { name: "浏览器" })).toBeTruthy();
    expect(screen.queryByRole("group", { name: "开始" })).toBeNull();
  });

  it("opens the file explorer from the launcher and the first change from the changes entry", async () => {
    const user = userEvent.setup();
    const changes = [{ path: "a.txt", status: "M" }] as never;
    const props = { port: new MockPort(), manual: true, shown: false, scheme: "dark" as const, changes, onCloseManual: vi.fn(), onSurfaces: vi.fn(), onExternal: vi.fn() };
    render(<WorkbenchPanel {...props} tabs={[]} />);
    await user.click(screen.getByRole("button", { name: /^改动/ }));
    expect(await screen.findByRole("tab", { name: /a\.txt/ })).toBeTruthy();
    expect(screen.queryByRole("group", { name: "开始" })).toBeNull();
  });

  it("gives way to the agent's page, with no launcher over it", () => {
    const props = { port: new MockPort(), manual: true, shown: false, scheme: "dark" as const, changes: [], onCloseManual: vi.fn(), onSurfaces: vi.fn(), onExternal: vi.fn() };
    const { rerender } = render(<WorkbenchPanel {...props} tabs={[]} />);
    expect(screen.getByRole("group", { name: "开始" })).toBeTruthy();
    rerender(<WorkbenchPanel {...props} tabs={[{ id: "b1", target: "t1", url: "https://top.baidu.com", title: "百度热搜", active: true }]} />);
    expect(screen.queryByRole("group", { name: "开始" })).toBeNull();
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

  it("keeps the column when its last tab closes while the explorer is open", async () => {
    const user = userEvent.setup();
    const onCloseManual = vi.fn();
    const page = { id: "b1", target: "t1", url: "https://example.com", title: "My page", active: true };
    const { container } = render(<WorkbenchPanel port={new MockPort()} tabs={[page]} manual shown={false} scheme="dark" changes={[]} onCloseManual={onCloseManual} onSurfaces={vi.fn()} onExternal={vi.fn()} />);
    await user.click(screen.getByRole("button", { name: "文件" }));
    await user.click(screen.getByRole("button", { name: "关闭 My page" }));
    expect(onCloseManual).not.toHaveBeenCalled();
    expect(container.querySelector(".workbench-body")?.hasAttribute("data-files")).toBe(true);
  });

  // A file opened from the explorer steps the list aside rather than closing it,
  // so closing that file comes back to the list. Folding the column instead
  // leaves someone who opened one file with no workbench at all. With no tabs
  // the launcher draws a "文件" card as well as the toolbar toggle, so the
  // toolbar button is named by its action rather than its label.
  it("returns to the explorer when a file opened from it is closed", async () => {
    const user = userEvent.setup();
    const onCloseManual = vi.fn();
    const { container } = render(<WorkbenchPanel port={new MockPort()} tabs={[]} manual={false} shown scheme="light" changes={[]} onCloseManual={onCloseManual} onSurfaces={vi.fn()} onExternal={vi.fn()} />);

    await user.click(container.querySelector<HTMLElement>('[data-action="workbench.files"]')!);
    expect(container.querySelector(".workbench-body")?.hasAttribute("data-files")).toBe(true);

    await user.click(await screen.findByRole("button", { name: "README.md" }));
    expect(screen.getByRole("tab", { name: "README.md" })).toBeTruthy();
    // The list stepped aside for the file.
    expect(container.querySelector(".workbench-body")?.hasAttribute("data-files")).toBe(false);

    await user.click(screen.getByRole("button", { name: "关闭 README.md" }));
    expect(onCloseManual).not.toHaveBeenCalled();
    expect(container.querySelector(".workbench-body")?.hasAttribute("data-files")).toBe(true);
  });

  // The list was stepped aside for that file, not closed on behalf of the one
  // behind it: closing the file leaves the other tab selected and alone, so the
  // list must stay out of its way and the column must not fold.
  it("does not bring the explorer back over a tab that is still open", async () => {
    const user = userEvent.setup();
    const onCloseManual = vi.fn();
    const page = { id: "b1", target: "t1", url: "https://example.com", title: "My page", active: false };
    const { container } = render(<WorkbenchPanel port={new MockPort()} tabs={[page]} manual={false} shown scheme="light" changes={[]} onCloseManual={onCloseManual} onSurfaces={vi.fn()} onExternal={vi.fn()} />);

    await user.click(container.querySelector<HTMLElement>('[data-action="workbench.files"]')!);
    await user.click(await screen.findByRole("button", { name: "README.md" }));
    expect(container.querySelector(".workbench-body")?.hasAttribute("data-files")).toBe(false);

    await user.click(screen.getByRole("button", { name: "关闭 README.md" }));
    expect(container.querySelector(".workbench-body")?.hasAttribute("data-files")).toBe(false);
    expect(onCloseManual).not.toHaveBeenCalled();
    expect(screen.getByRole("tab", { name: "My page" }).getAttribute("aria-selected")).toBe("true");
  });

  // Closing every browser tab at once while a file from the explorer is still
  // open leaves that file behind, so the column stays and the list stays away.
  // The batch goes through the same rule as a single close, which is the point.
  it("keeps the column when every browser tab is closed at once beside an explorer file", async () => {
    const user = userEvent.setup();
    const onCloseManual = vi.fn();
    const pages = [
      { id: "b1", target: "t1", url: "https://top.baidu.com", title: "Agent page", active: false },
      { id: "b2", target: "t2", url: "https://example.com", title: "My page", active: false },
    ];
    const { container } = render(<WorkbenchPanel port={new MockPort()} tabs={pages} manual={false} shown scheme="light" changes={[]} onCloseManual={onCloseManual} onSurfaces={vi.fn()} onExternal={vi.fn()} />);

    await user.click(container.querySelector<HTMLElement>('[data-action="workbench.files"]')!);
    await user.click(await screen.findByRole("button", { name: "README.md" }));
    expect(screen.getByRole("tab", { name: "README.md" })).toBeTruthy();

    await user.pointer({ keys: "[MouseRight]", target: screen.getByRole("tab", { name: "My page" }) });
    await user.click(await screen.findByRole("menuitem", { name: "关闭全部浏览器标签" }));

    expect(screen.queryByRole("tab", { name: "My page" })).toBeNull();
    expect(screen.queryByRole("tab", { name: "Agent page" })).toBeNull();
    expect(screen.getByRole("tab", { name: "README.md" })).toBeTruthy();
    expect(onCloseManual).not.toHaveBeenCalled();
    expect(container.querySelector(".workbench-body")?.hasAttribute("data-files")).toBe(false);
  });

  it("shows a page the person opened beside the agent's, while the agent's tab stays active", () => {
    const props = { port: new MockPort(), manual: true, shown: false, scheme: "dark" as const, changes: [], onCloseManual: vi.fn(), onSurfaces: vi.fn(), onExternal: vi.fn() };
    const agent = { id: "b1", target: "t1", url: "https://top.baidu.com", title: "百度热搜", active: true };
    const { rerender } = render(<WorkbenchPanel {...props} tabs={[agent]} />);
    rerender(<WorkbenchPanel {...props} tabs={[agent, { id: "b2", target: "t2", url: "https://example.com", title: "Link", active: false }]} />);
    expect(screen.getByRole("tab", { name: "Link" }).getAttribute("aria-selected")).toBe("true");
  });

  it("selects a link's page again after the previous one was closed, with the agent's tab still active", async () => {
    const user = userEvent.setup();
    const props = { port: new MockPort(), manual: true, shown: false, scheme: "dark" as const, changes: [], onCloseManual: vi.fn(), onSurfaces: vi.fn(), onExternal: vi.fn() };
    const agent = { id: "b1", target: "t1", url: "https://top.baidu.com", title: "Agent page", active: true };
    const first = { id: "b2", target: "t2", url: "https://example.com", title: "First", active: false };
    const second = { id: "b3", target: "t3", url: "https://example.org", title: "Second", active: false };
    const { rerender } = render(<WorkbenchPanel {...props} tabs={[agent]} />);
    rerender(<WorkbenchPanel {...props} tabs={[agent, first]} />);
    expect(screen.getByRole("tab", { name: "First" }).getAttribute("aria-selected")).toBe("true");
    await user.click(screen.getByRole("button", { name: "关闭 First" }));
    rerender(<WorkbenchPanel {...props} tabs={[agent]} />);
    rerender(<WorkbenchPanel {...props} tabs={[agent, second]} />);
    expect(screen.getByRole("tab", { name: "Second" }).getAttribute("aria-selected")).toBe("true");
  });

  it("lists dot folders only once hidden files are asked for, and remembers the choice", async () => {
    const user = userEvent.setup();
    localStorage.removeItem("rx-show-hidden-files");
    const port = new MockPort();
    const list = vi.spyOn(port, "workspaceFiles");
    const props = { port, tabs: [], manual: false, shown: true, scheme: "light" as const, changes: [], onCloseManual: vi.fn(), onSurfaces: vi.fn(), onExternal: vi.fn() };
    const { unmount } = render(<WorkbenchPanel {...props} />);
    await screen.findByRole("button", { name: "README.md" });
    expect(screen.queryByRole("button", { name: ".reasonix" })).toBeNull();
    expect(list).toHaveBeenLastCalledWith("", "", false);
    const toggle = screen.getByRole("button", { name: "显示隐藏文件" });
    expect(toggle.getAttribute("aria-pressed")).toBe("false");
    await user.click(toggle);
    expect(await screen.findByRole("button", { name: ".reasonix" })).toBeTruthy();
    expect(list).toHaveBeenLastCalledWith("", "", true);
    unmount();
    render(<WorkbenchPanel {...props} />);
    expect(await screen.findByRole("button", { name: ".reasonix" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "显示隐藏文件" }).getAttribute("aria-pressed")).toBe("true");
    localStorage.removeItem("rx-show-hidden-files");
  });

  it("shows the file that was picked from the list rather than leaving the list over it", async () => {
    const user = userEvent.setup();
    const { container } = render(<WorkbenchPanel port={new MockPort()} tabs={[]} manual={false} shown scheme="light" changes={[]} onCloseManual={vi.fn()} onSurfaces={vi.fn()} onExternal={vi.fn()} />);
    await user.click(screen.getAllByRole("button", { name: "文件" })[0]);
    expect(container.querySelector(".workbench-body")?.hasAttribute("data-files")).toBe(true);
    await user.click(await screen.findByRole("button", { name: "README.md" }));
    expect(container.querySelector(".workbench-body")?.hasAttribute("data-files")).toBe(false);
    expect(screen.getByRole("tab", { name: "README.md" }).getAttribute("aria-selected")).toBe("true");
  });

  it("steps the file list aside when a browser tab is picked", async () => {
    const user = userEvent.setup();
    const pages = [
      { id: "b1", target: "t1", url: "https://top.baidu.com", title: "Agent page", active: true },
      { id: "b2", target: "t2", url: "https://example.com", title: "My page", active: false },
    ];
    const { container } = render(<WorkbenchPanel port={new MockPort()} tabs={pages} manual shown={false} scheme="dark" changes={[]} onCloseManual={vi.fn()} onSurfaces={vi.fn()} onExternal={vi.fn()} />);
    await user.click(screen.getAllByRole("button", { name: "文件" })[0]);
    expect(container.querySelector(".workbench-body")?.hasAttribute("data-files")).toBe(true);
    await user.click(screen.getByRole("tab", { name: "Agent page" }).querySelector("button") ?? screen.getByRole("tab", { name: "Agent page" }));
    expect(container.querySelector(".workbench-body")?.hasAttribute("data-files")).toBe(false);
    expect(screen.getByRole("tab", { name: "Agent page" }).getAttribute("aria-selected")).toBe("true");
  });

  it("steps the file list aside when a new browser tab is opened", async () => {
    const user = userEvent.setup();
    const { container } = render(<WorkbenchPanel port={new MockPort()} tabs={[]} manual={false} shown scheme="light" changes={[]} onCloseManual={vi.fn()} onSurfaces={vi.fn()} onExternal={vi.fn()} />);
    await user.click(screen.getAllByRole("button", { name: "文件" })[0]);
    expect(container.querySelector(".workbench-body")?.hasAttribute("data-files")).toBe(true);
    await user.click(screen.getByRole("button", { name: "新建标签" }));
    await waitFor(() => expect(container.querySelector(".workbench-body")?.hasAttribute("data-files")).toBe(false));
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

  it("reads the tree again when a call that may write finishes mid-turn", async () => {
    const disk = new Set(["notes.md"]);
    const port = new MockPort();
    vi.spyOn(port, "workspaceFiles").mockImplementation(async () => ({ files: [...disk], directories: [] }));
    const props = { port, tabs: [], manual: false, shown: true, scheme: "light" as const, changes: [], running: true, onCloseManual: vi.fn(), onSurfaces: vi.fn(), onExternal: vi.fn() };
    const { rerender } = render(<WorkbenchPanel {...props} wrote={0} />);
    await screen.findByRole("button", { name: "notes.md" });

    disk.add("draft.txt");
    rerender(<WorkbenchPanel {...props} wrote={1} />);

    expect(await screen.findByRole("button", { name: "draft.txt" })).toBeTruthy();
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

  it("shows the open file in the system file manager from the file's own toolbar", async () => {
    const user = userEvent.setup();
    const port = new MockPort();
    const reveal = vi.spyOn(port, "revealInFileManager");
    render(<WorkbenchPanel port={port} tabs={[]} manual={false} shown scheme="light" changes={[]} onCloseManual={vi.fn()} onSurfaces={vi.fn()} onExternal={vi.fn()} />);
    await user.click(await screen.findByRole("button", { name: "README.md" }));
    await user.click(await screen.findByRole("button", { name: "在系统文件管理器中显示此文件" }));
    expect(reveal).toHaveBeenLastCalledWith("README.md");
  });

  it("says why showing the open file was refused, next to the file", async () => {
    const user = userEvent.setup();
    const port = new MockPort();
    vi.spyOn(port, "revealInFileManager").mockRejectedValue(
      new HttpError(403, "no window", { code: "workspace.locate_no_window", error: "no window" }),
    );
    render(<WorkbenchPanel port={port} tabs={[]} manual={false} shown scheme="light" changes={[]} onCloseManual={vi.fn()} onSurfaces={vi.fn()} onExternal={vi.fn()} />);
    await user.click(await screen.findByRole("button", { name: "README.md" }));
    await user.click(await screen.findByRole("button", { name: "在系统文件管理器中显示此文件" }));
    expect((await screen.findByRole("alert")).textContent).toBe("这个内核不在本机，没法在系统文件管理器中显示它的文件。");
  });

  it("offers no file-manager button on a remote workspace's file", async () => {
    const user = userEvent.setup();
    const port = new MockPort();
    render(<WorkbenchPanel port={port} tabs={[]} manual={false} shown scheme="light" changes={[]} remote onCloseManual={vi.fn()} onSurfaces={vi.fn()} onExternal={vi.fn()} />);
    await user.click(await screen.findByRole("button", { name: "README.md" }));
    await screen.findByRole("button", { name: "编辑" });
    expect(screen.queryByRole("button", { name: "在系统文件管理器中显示此文件" })).toBeNull();
  });

  it("offers no way to reveal a remote workspace's paths on this machine", async () => {
    const port = new MockPort();
    const reveal = vi.spyOn(port, "revealInFileManager");
    render(<WorkbenchPanel port={port} tabs={[]} manual={false} shown scheme="light" changes={[]} remote onCloseManual={vi.fn()} onSurfaces={vi.fn()} onExternal={vi.fn()} />);
    const row = await screen.findByRole("button", { name: "README.md" });

    expect(screen.queryByRole("button", { name: "在系统文件管理器中显示工作区" })).toBeNull();
    expect(fireEvent.contextMenu(row)).toBe(false);
    expect(screen.queryByRole("menuitem", { name: /文件管理器/ })).toBeNull();
    expect(screen.getByRole("menuitem", { name: "复制引用" })).toBeTruthy();
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
    const { port, view, land } = await openWithHeldRead();
    vi.spyOn(port, "saveWorkspaceFile").mockImplementation(async (file) => ({ ...file, revision: "r3" }));

    act(() => view.dispatch({ changes: { from: view.state.doc.length, insert: "\n\n## Saved" } }));
    fireEvent.keyDown(window, { key: "s", ctrlKey: true });
    await waitFor(() => expect(port.saveWorkspaceFile).toHaveBeenCalled());
    await land({ content: "# Second", revision: "r2" });

    expect(view.state.doc.toString()).toBe("# First\n\n## Saved");
  });
});

describe("opening the project in other programs from the file panel", () => {
  it("offers the menu in the explorer head and on an open file, and not on a remote workspace", async () => {
    const user = userEvent.setup();
    const port = new MockPort();
    const hub = { openWith: vi.fn().mockResolvedValue(undefined), openWithTargets: vi.fn().mockResolvedValue([{ id: "terminal", name: "终端", available: true }]) };
    vi.spyOn(hostModule, "host").mockReturnValue({ revealsFiles: () => true, describe: async () => ({ platform: "darwin" }) } as never);
    const props = { port, tabs: [], manual: false, shown: true, scheme: "light" as const, changes: [], onCloseManual: vi.fn(), onSurfaces: vi.fn(), onExternal: vi.fn(), workspaceRoot: "/w", hub };
    const view = render(<WorkbenchPanel {...props} />);
    await user.click(screen.getAllByRole("button", { name: "用其他程序打开项目" })[0]!);
    await user.click(await screen.findByRole("menuitem", { name: "用 终端 打开" }));
    expect(hub.openWith).toHaveBeenCalledWith("/w", "terminal");
    view.rerender(<WorkbenchPanel {...props} remote />);
    expect(screen.queryByRole("button", { name: "用其他程序打开项目" })).toBeNull();
  });
});

describe("naming a built-in browser tab", () => {
  beforeEach(() => vi.restoreAllMocks());
  const page = { id: "p", target: "tp", url: "https://example.com/a/very/long/path?x=1", title: "", active: true };
  const draw = () => {
    const port = new MockPort();
    const props = { port, manual: false, shown: true, scheme: "light" as const, changes: [], onCloseManual: vi.fn(), onSurfaces: vi.fn(), onExternal: vi.fn() };
    return render(<WorkbenchPanel {...props} tabs={[page]} />);
  };

  it("shows the address until the person names the tab, and keeps the name across a title change", async () => {
    const user = userEvent.setup();
    const view = draw();
    const tab = screen.getByRole("tab", { name: page.url });
    await user.dblClick(within(tab).getByText(page.url));
    const field = screen.getByRole("textbox", { name: "重命名页签" });
    await user.clear(field);
    await user.type(field, "文档{Enter}");
    expect(screen.getByRole("tab", { name: "文档" })).toBeTruthy();
    expect(screen.queryByRole("textbox", { name: "重命名页签" })).toBeNull();
    // The page's own title arriving later does not take the name back.
    view.rerender(<WorkbenchPanel port={new MockPort()} manual={false} shown scheme="light" changes={[]} onCloseManual={vi.fn()} onSurfaces={vi.fn()} onExternal={vi.fn()} tabs={[{ ...page, title: "Example Domain" }]} />);
    expect(screen.getByRole("tab", { name: "文档" })).toBeTruthy();
  });

  it("opening the field and leaving it unchanged does not freeze the page's title", async () => {
    const user = userEvent.setup();
    const view = draw();
    await user.dblClick(within(screen.getByRole("tab", { name: page.url })).getByText(page.url));
    await user.keyboard("{Enter}");
    view.rerender(<WorkbenchPanel port={new MockPort()} manual={false} shown scheme="light" changes={[]} onCloseManual={vi.fn()} onSurfaces={vi.fn()} onExternal={vi.fn()} tabs={[{ ...page, title: "Example Domain" }]} />);
    expect(screen.getByRole("tab", { name: "Example Domain" })).toBeTruthy();
  });

  it("F2 on a focused browser tab opens the same field from the keyboard", async () => {
    const user = userEvent.setup();
    draw();
    within(screen.getByRole("tab", { name: page.url })).getByRole("button", { name: page.url }).focus();
    await user.keyboard("{F2}");
    expect(screen.getByRole("textbox", { name: "重命名页签" })).toBeTruthy();
  });

  it("Escape keeps the old label, and an emptied name gives the page's own label back", async () => {
    const user = userEvent.setup();
    draw();
    await user.dblClick(within(screen.getByRole("tab", { name: page.url })).getByText(page.url));
    await user.type(screen.getByRole("textbox", { name: "重命名页签" }), "x{Escape}");
    expect(screen.getByRole("tab", { name: page.url })).toBeTruthy();
    await user.dblClick(within(screen.getByRole("tab", { name: page.url })).getByText(page.url));
    await user.clear(screen.getByRole("textbox", { name: "重命名页签" }));
    await user.type(screen.getByRole("textbox", { name: "重命名页签" }), "名字{Enter}");
    await user.dblClick(within(screen.getByRole("tab", { name: "名字" })).getByText("名字"));
    await user.clear(screen.getByRole("textbox", { name: "重命名页签" }));
    await user.keyboard("{Enter}");
    expect(screen.getByRole("tab", { name: page.url })).toBeTruthy();
  });
});
