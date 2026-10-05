// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import "./testkit";
import { Composer } from "./Composer";
import { MockPort } from "../port/mock";
import { DeliveryError, HttpError } from "../port/port";
import type { AgentPort, ApprovalMode, Attachment, Completion, ModelEntry, Preset, SessionStatus, WorkspaceGit } from "../port/port";
import { draftKey } from "./drafts";

afterEach(() => { cleanup(); localStorage.clear(); vi.restoreAllMocks(); });

// The workspace's git answer, the way /workspace/git words it. Tests that name
// an override get it; the rest get a plain repository on main.
const git = (over: Partial<WorkspaceGit> = {}): WorkspaceGit =>
  ({ repo: true, name: "reasonix", branch: "main", detached: false, added: 0, removed: 0, untracked: 0, ...over });

const status = (over: Partial<SessionStatus> = {}) =>
  ({
    preset: "balanced" as Preset,
    effort: "auto",
    toolApprovalMode: "ask" as ApprovalMode,
    plan: false,
    modelRef: "deepseek/deepseek-v4-pro",
    ...over,
  }) as SessionStatus;

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((done) => {
    resolve = done;
  });
  return { promise, resolve };
}

function touchPointer() {
  vi.spyOn(window, "matchMedia").mockImplementation((query: string) => ({
    matches: query === "(pointer: coarse)",
    media: query,
    onchange: null,
    addListener() {},
    removeListener() {},
    addEventListener() {},
    removeEventListener() {},
    dispatchEvent: () => false,
  }) as MediaQueryList);
}

function draw(
  over: { running?: boolean; onSubmit?: (text: string) => Promise<boolean>; port?: MockPort; st?: SessionStatus; changeCount?: number; host?: string; git?: WorkspaceGit | null; onTreeChanged?: () => void } = {},
) {
  const port = over.port ?? new MockPort();
  const onSubmit = over.onSubmit ?? vi.fn(async () => true);
  const st = over.st ?? status();
  const view = render(
    <Composer
      port={port as unknown as AgentPort}
      status={st}
      running={over.running ?? false}
      focus={0}
      onSubmit={onSubmit}
      onChanged={vi.fn()}
      onError={vi.fn()}
      changeCount={over.changeCount}
      git={"git" in over ? over.git : git()}
      onTreeChanged={over.onTreeChanged}
      draftKey={draftKey(over.host ?? "", st.workspaceRoot ?? "/workspace", st.sessionPath ?? "")}
    />,
  );
  const box = view.container.querySelector('textarea[aria-label="任务输入"]') as HTMLTextAreaElement;
  return { ...view, port, onSubmit, box };
}

describe("composer submission", () => {
  it("disables a send that has nothing to send", () => {
    const { box } = draw();
    expect((screen.getByRole("button", { name: "发送" }) as HTMLButtonElement).disabled).toBe(true);
    fireEvent.change(box, { target: { value: "检查这次改动", selectionStart: 6 } });
    expect((screen.getByRole("button", { name: "发送" }) as HTMLButtonElement).disabled).toBe(false);
  });

  it("keeps Enter in the textarea when a touch-first pointer has focus", () => {
    touchPointer();
    const { box, onSubmit } = draw();
    fireEvent.change(box, { target: { value: "两行输入", selectionStart: 4 } });
    expect(fireEvent.keyDown(box, { key: "Enter" })).toBe(true);
    expect(onSubmit).not.toHaveBeenCalled();
    expect(screen.getByText("点按发送 · 回车换行")).toBeTruthy();
    expect(box.hasAttribute("aria-keyshortcuts")).toBe(false);
  });

  it("still submits through the Send button on a touch-first pointer", async () => {
    touchPointer();
    const { box, onSubmit } = draw();
    fireEvent.change(box, { target: { value: "点按发送", selectionStart: 4 } });
    fireEvent.keyDown(box, { key: "Enter" });
    expect(onSubmit).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "发送" }));
    await waitFor(() => expect(onSubmit).toHaveBeenCalledWith("点按发送"));
  });

  it("shows the tap-to-steer hint on a touch-first pointer during a live turn", () => {
    touchPointer();
    const { box, onSubmit } = draw({ running: true });
    fireEvent.change(box, { target: { value: "补充一句", selectionStart: 4 } });
    expect(fireEvent.keyDown(box, { key: "Enter" })).toBe(true);
    expect(onSubmit).not.toHaveBeenCalled();
    expect(screen.getByText("点按插话 · 回车换行")).toBeTruthy();
  });

  it("locks repeated Enter presses until the first submit settles", async () => {
    const pending = deferred<boolean>();
    const onSubmit = vi.fn(() => pending.promise);
    const { box } = draw({ onSubmit });
    fireEvent.change(box, { target: { value: "只发一次", selectionStart: 4 } });
    fireEvent.keyDown(box, { key: "Enter" });
    fireEvent.keyDown(box, { key: "Enter" });
    expect(onSubmit).toHaveBeenCalledTimes(1);
    expect(box.getAttribute("aria-busy")).toBe("true");
    await act(async () => pending.resolve(true));
  });

  it("restores the exact draft and caret when the host refuses it", async () => {
    const pending = deferred<boolean>();
    const { box } = draw({ onSubmit: () => pending.promise });
    fireEvent.change(box, { target: { value: "保留这份草稿", selectionStart: 3 } });
    box.setSelectionRange(3, 3);
    fireEvent.click(screen.getByRole("button", { name: "发送" }));
    await act(async () => pending.resolve(false));
    await waitFor(() => expect(box.value).toBe("保留这份草稿"));
    expect(box.selectionStart).toBe(3);
    expect(document.activeElement).toBe(box);
  });

  it("restores a folded long paste as a chip instead of flattening it", async () => {
    const pending = deferred<boolean>();
    const { box } = draw({ onSubmit: () => pending.promise });
    const body = Array.from({ length: 81 }, (_, i) => `line ${i}`).join("\n");
    fireEvent.paste(box, { clipboardData: { files: [], getData: () => body } });
    expect(screen.getByText("81 行 · 展开到输入框")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "发送" }));
    await act(async () => pending.resolve(false));
    expect(await screen.findByText("81 行 · 展开到输入框")).toBeTruthy();
    expect(box.value).toBe("");
  });
});

describe("composer drafts", () => {
  it("restores unsent text when a session is reopened", () => {
    const first = draw({ st: status({ sessionPath: "/sessions/one.jsonl" }) });
    fireEvent.change(first.box, { target: { value: "继续写这段", selectionStart: 6 } });
    first.unmount();

    const reopened = draw({ st: status({ sessionPath: "/sessions/one.jsonl" }) });
    expect(reopened.box.value).toBe("继续写这段");
  });

  it("keeps drafts apart by session and remote host", () => {
    const one = status({ sessionPath: "/sessions/one.jsonl" });
    const first = draw({ st: one });
    fireEvent.change(first.box, { target: { value: "仅本机会话一", selectionStart: 6 } });
    first.unmount();

    const otherSession = draw({ st: status({ sessionPath: "/sessions/two.jsonl" }) });
    expect(otherSession.box.value).toBe("");
    otherSession.unmount();
    const remote = draw({ st: one, host: "remote-a" });
    expect(remote.box.value).toBe("");
    remote.unmount();
    const reopened = draw({ st: one });
    expect(reopened.box.value).toBe("仅本机会话一");
  });

  it("removes a draft only after the host accepts its submission", async () => {
    const st = status({ sessionPath: "/sessions/one.jsonl" });
    const first = draw({ st });
    fireEvent.change(first.box, { target: { value: "发出去", selectionStart: 3 } });
    first.unmount();
    const reopened = draw({ st });
    fireEvent.click(screen.getByRole("button", { name: "发送" }));
    await waitFor(() => expect(localStorage.getItem(draftKey("", "/workspace", st.sessionPath!))).toBeNull());
    reopened.unmount();
    expect(draw({ st }).box.value).toBe("");
  });

  it("binds text typed before a new session receives its path", () => {
    const port = new MockPort() as unknown as AgentPort;
    const props = { port, status: status(), running: false, onSubmit: async () => true, onChanged: vi.fn(), onError: vi.fn() };
    const view = render(<Composer {...props} />);
    const box = view.container.querySelector("textarea") as HTMLTextAreaElement;
    fireEvent.change(box, { target: { value: "新会话的草稿", selectionStart: 6 } });
    const path = "/sessions/new.jsonl";
    view.rerender(<Composer {...props} status={status({ sessionPath: path })} draftKey={draftKey("", "/workspace", path)} />);
    view.unmount();
    expect(draw({ st: status({ sessionPath: path }) }).box.value).toBe("新会话的草稿");
  });

  it("flushes the latest line when the page closes before the debounce", () => {
    const st = status({ sessionPath: "/sessions/one.jsonl" });
    const first = draw({ st });
    fireEvent.change(first.box, { target: { value: "刚输入的字", selectionStart: 5 } });
    window.dispatchEvent(new Event("pagehide"));
    expect(localStorage.getItem(draftKey("", "/workspace", st.sessionPath!))).toBe("刚输入的字");
  });

  it("keeps the pending text durable until submission succeeds", async () => {
    const pending = deferred<boolean>();
    const st = status({ sessionPath: "/sessions/one.jsonl" });
    const first = draw({ st, onSubmit: () => pending.promise });
    fireEvent.change(first.box, { target: { value: "等待服务器确认", selectionStart: 7 } });
    fireEvent.click(screen.getByRole("button", { name: "发送" }));
    window.dispatchEvent(new Event("pagehide"));
    const key = draftKey("", "/workspace", st.sessionPath!);
    expect(localStorage.getItem(key)).toBe("等待服务器确认");
    await act(async () => pending.resolve(true));
    expect(localStorage.getItem(key)).toBeNull();
  });
});

describe("composer run controls", () => {
  it.each([false, true])("shows steering only with content during a live turn (pending ask: %s)", async (asking) => {
    const port = new MockPort();
    const cancel = vi.spyOn(port, "cancel").mockResolvedValue();
    const onSubmit = vi.fn(async () => true);
    const { box } = draw({ port, running: true, onSubmit,
      st: status({ running: true, decisions: asking ? [{ id: "ask-1", kind: "ask" }] : [] }),
    });
    expect(screen.queryByRole("button", { name: "插话" })).toBeNull();
    expect((screen.getByRole("button", { name: "停下" }) as HTMLButtonElement).disabled).toBe(false);
    fireEvent.change(box, { target: { value: "   " } });
    expect(screen.queryByRole("button", { name: "插话" })).toBeNull();
    fireEvent.change(box, { target: { value: "Check" } });
    expect((screen.getByRole("button", { name: "插话" }) as HTMLButtonElement).disabled).toBe(false);
    expect(screen.getByRole("button", { name: "停下" })).toBeTruthy();
    fireEvent.change(box, { target: { value: "" } });
    expect(screen.queryByRole("button", { name: "插话" })).toBeNull();
    fireEvent.change(box, { target: { value: "先检查日志", selectionStart: 5 } });
    fireEvent.click(screen.getByRole("button", { name: "插话" }));
    await waitFor(() => expect(onSubmit).toHaveBeenCalledWith("先检查日志"));
    await waitFor(() => expect(screen.queryByRole("button", { name: "插话" })).toBeNull());
    fireEvent.click(screen.getByRole("button", { name: "停下" }));
    await waitFor(() => expect(cancel).toHaveBeenCalledTimes(1));
  });

  it("allows attachment-only steering during a live turn", async () => {
    const port = new MockPort();
    vi.spyOn(port, "attach").mockResolvedValue({ path: ".reasonix/attachments/note.txt", ref: "@note.txt", image: false });
    const { container, onSubmit } = draw({ port, running: true });
    const input = container.querySelector('input[type="file"]') as HTMLInputElement;
    fireEvent.change(input, { target: { files: [new File(["note"], "note.txt", { type: "text/plain" })] } });
    const send = await screen.findByRole("button", { name: "插话" });
    expect((send as HTMLButtonElement).disabled).toBe(false);
    expect(screen.getByRole("button", { name: "停下" })).toBeTruthy();
    fireEvent.click(send);
    await waitFor(() => expect(onSubmit).toHaveBeenCalledWith("@note.txt"));
  });

  it("does not take Shift+Tab away from reverse focus navigation", () => {
    const port = new MockPort();
    const plan = vi.spyOn(port, "setPlanMode");
    const { box } = draw({ port });
    fireEvent.keyDown(box, { key: "Tab", shiftKey: true });
    expect(plan).not.toHaveBeenCalled();
  });

  it("keeps stop pending until the live turn actually ends", async () => {
    const port = new MockPort();
    const cancel = vi.spyOn(port, "cancel").mockResolvedValue();
    const props = {
      port: port as unknown as AgentPort,
      status: status(),
      focus: 0,
      onSubmit: vi.fn(async () => true),
      onChanged: vi.fn(),
      onError: vi.fn(),
    };
    const view = render(<Composer {...props} running />);
    fireEvent.click(screen.getByRole("button", { name: "停下" }));
    const pending = await screen.findByRole("button", { name: "正在停止…" });
    expect((pending as HTMLButtonElement).disabled).toBe(true);
    fireEvent.click(pending);
    expect(cancel).toHaveBeenCalledTimes(1);
    expect(screen.queryByRole("button", { name: "插话" })).toBeNull();
    view.rerender(<Composer {...props} running={false} />);
    await waitFor(() => expect(screen.queryByRole("button", { name: "正在停止…" })).toBeNull());
  });
});

describe("composer stop under a starved window", () => {
  const props = (port: MockPort, over: Partial<{ onError: (e: unknown) => void; onChanged: () => void }> = {}) => ({
    port: port as unknown as AgentPort,
    focus: 0,
    onSubmit: vi.fn(async () => true),
    onChanged: over.onChanged ?? vi.fn(),
    onError: over.onError ?? vi.fn(),
  });

  it("ends a stop the kernel confirmed when the turn-done event never arrives", async () => {
    const port = new MockPort();
    vi.spyOn(port, "cancel").mockResolvedValue();
    const p = props(port);
    const view = render(<Composer {...p} status={status({ running: true })} running />);
    fireEvent.click(screen.getByRole("button", { name: "停下" }));
    await screen.findByRole("button", { name: "正在停止…" });
    await waitFor(() => expect(p.onChanged).toHaveBeenCalled());
    view.rerender(<Composer {...p} status={status({ running: false })} running />);
    await waitFor(() => expect(screen.queryByRole("button", { name: "正在停止…" })).toBeNull());
    expect((screen.getByRole("button", { name: "停下" }) as HTMLButtonElement).disabled).toBe(false);
  });

  it("does not end a stop while the kernel still reports the turn live", async () => {
    const port = new MockPort();
    vi.spyOn(port, "cancel").mockResolvedValue();
    const p = props(port);
    const view = render(<Composer {...p} status={status({ running: true })} running />);
    fireEvent.click(screen.getByRole("button", { name: "停下" }));
    await screen.findByRole("button", { name: "正在停止…" });
    view.rerender(<Composer {...p} status={status({ running: true, plan: true })} running />);
    expect(screen.getByRole("button", { name: "正在停止…" })).toBeTruthy();
  });

  it.each(["kernel_busy", "unreachable", "ui_stalled"] as const)("hands back an unconfirmed stop as a %s fault instead of staying pending", async (fault) => {
    const port = new MockPort();
    const err = new DeliveryError(fault);
    vi.spyOn(port, "cancel").mockRejectedValue(err);
    const p = props(port);
    render(<Composer {...p} status={status({ running: true })} running />);
    fireEvent.click(screen.getByRole("button", { name: "停下" }));
    await waitFor(() => expect(p.onError).toHaveBeenCalledWith(err));
    expect((screen.getByRole("button", { name: "停下" }) as HTMLButtonElement).disabled).toBe(false);
    expect(p.onChanged).not.toHaveBeenCalled();
  });

  it("lets a stop be asked again after it was left unconfirmed", async () => {
    const port = new MockPort();
    const cancel = vi.spyOn(port, "cancel").mockRejectedValueOnce(new DeliveryError("ui_stalled")).mockResolvedValue();
    const p = props(port);
    render(<Composer {...p} status={status({ running: true })} running />);
    fireEvent.click(screen.getByRole("button", { name: "停下" }));
    await waitFor(() => expect(p.onError).toHaveBeenCalled());
    fireEvent.click(screen.getByRole("button", { name: "停下" }));
    await waitFor(() => expect(cancel).toHaveBeenCalledTimes(2));
  });
});

describe("composer menus", () => {
  it("shows that slash skills are loading until the catalog answers", async () => {
    vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout"] });
    try {
      const port = new MockPort();
      const request = deferred<Completion>();
      vi.spyOn(port, "complete").mockReturnValue(request.promise);
      const { box } = draw({ port });
      const status = screen.getByRole("status", { name: "补全" });
      fireEvent.change(box, { target: { value: "/", selectionStart: 1 } });
      expect(screen.queryByText("正在加载命令与技能…")).toBeNull();
      act(() => vi.advanceTimersByTime(180));
      expect(screen.getByText("正在加载命令与技能…")).toBeTruthy();
      expect(screen.getByRole("status", { name: "补全" })).toBe(status);
      await act(async () => request.resolve({
        kind: "slash", from: 0, to: 1, query: "/",
        items: [{ label: "/review", insert: "/review ", kind: "skill" }],
      }));
      expect(screen.queryByText("正在加载命令与技能…")).toBeNull();
      expect(screen.getByRole("option", { name: /review/ })).toBeTruthy();
      expect(screen.getByRole("status", { name: "补全" })).toBe(status);
    } finally {
      vi.useRealTimers();
    }
  });

  it("does not flash loading for a fast slash catalog answer", async () => {
    vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout"] });
    try {
      const port = new MockPort();
      const { box } = draw({ port });
      fireEvent.change(box, { target: { value: "/", selectionStart: 1 } });
      await act(async () => {});
      act(() => vi.advanceTimersByTime(180));
      expect(screen.queryByText("正在加载命令与技能…")).toBeNull();
      expect(screen.getByRole("listbox", { name: "补全" })).toBeTruthy();
    } finally {
      vi.useRealTimers();
    }
  });

  it("dismisses a pending slash catalog without reopening on its late answer", async () => {
    vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout"] });
    try {
      const port = new MockPort();
      const request = deferred<Completion>();
      vi.spyOn(port, "complete").mockReturnValue(request.promise);
      const { box } = draw({ port });
      fireEvent.change(box, { target: { value: "/", selectionStart: 1 } });
      act(() => vi.advanceTimersByTime(180));
      expect(screen.getByText("正在加载命令与技能…")).toBeTruthy();
      fireEvent.keyDown(box, { key: "Escape" });
      expect(screen.queryByText("正在加载命令与技能…")).toBeNull();
      await act(async () => request.resolve({
        kind: "slash", from: 0, to: 1, query: "/",
        items: [{ label: "/review", insert: "/review ", kind: "skill" }],
      }));
      expect(screen.queryByRole("option", { name: /review/ })).toBeNull();
    } finally {
      vi.useRealTimers();
    }
  });

  it("keeps the completion list mounted while refining a slash query", async () => {
    const port = new MockPort();
    const next = deferred<Completion>();
    const complete = vi.spyOn(port, "complete");
    const { box } = draw({ port });
    fireEvent.change(box, { target: { value: "/", selectionStart: 1 } });
    const list = await screen.findByRole("listbox", { name: "补全" });
    complete.mockReturnValueOnce(next.promise);
    fireEvent.change(box, { target: { value: "/r", selectionStart: 2 } });
    expect(screen.getByRole("listbox", { name: "补全" })).toBe(list);
    expect(screen.queryByText("正在加载命令与技能…")).toBeNull();
    await act(async () => next.resolve({
      kind: "slash", from: 0, to: 2, query: "/r",
      items: [{ label: "/review", insert: "/review ", kind: "skill" }],
    }));
    expect(screen.getByRole("listbox", { name: "补全" })).toBe(list);
  });

  it("keeps the file completion list mounted during the next lookup", async () => {
    const port = new MockPort();
    const next = deferred<Completion>();
    const complete = vi.spyOn(port, "complete");
    const { box } = draw({ port });
    fireEvent.change(box, { target: { value: "@", selectionStart: 1 } });
    const list = await screen.findByRole("listbox", { name: "补全" });
    complete.mockReturnValueOnce(next.promise);
    fireEvent.change(box, { target: { value: "@R", selectionStart: 2 } });
    expect(screen.getByRole("listbox", { name: "补全" })).toBe(list);
    await act(async () => next.resolve({
      kind: "ref", from: 0, to: 2, query: "R",
      items: [{ label: "README.md", insert: "@README.md", kind: "file" }],
    }));
    expect(screen.getByRole("listbox", { name: "补全" })).toBe(list);
  });

  it("shows real workspace changes beside the current branch", async () => {
    draw({ changeCount: 3 });
    expect(await screen.findByText("3 个变更")).toBeTruthy();
    expect(await screen.findByText("main")).toBeTruthy();
  });

  // The reading is the tree's, not the capability scope's project identity: a
  // detached HEAD names its commit, and the menu marks nothing as current.
  it("names a detached HEAD by its commit and marks no menu row", async () => {
    draw({ git: git({ branch: "4f2c1ab", detached: true }) });
    const chip = await screen.findByText("4f2c1ab");
    fireEvent.click(chip.closest("button")!);
    expect(await screen.findByText("本地分支")).toBeTruthy();
    // The menu rows carry the trigger's action id, so the assertion reads the
    // branch menu and not whichever other menu is mounted.
    expect(document.querySelector('[data-action="git.branch"][data-on]')).toBeNull();
  });

  it("says when the workspace has no repository instead of vanishing", () => {
    draw({ git: git({ repo: false }) });
    expect(screen.getByRole("img", { name: "非 Git 仓库" })).toBeTruthy();
  });

  // Not answered yet is not the same fact as not a repository, and the two
  // must not render as one word.
  it("renders nothing before git has answered", () => {
    draw({ git: null });
    expect(document.querySelector(".studio-branch")).toBeNull();
  });

  it("keeps the branch trigger named and its refresh tooltip reachable by keyboard", async () => {
    draw();
    const chip = await screen.findByRole("button", { name: "当前 Git 分支：main" });
    chip.focus();
    expect(document.activeElement).toBe(chip);
    const tooltip = document.getElementById(chip.getAttribute("aria-describedby") ?? "");
    expect(tooltip?.textContent).toContain("非实时");
  });

  it("lists local branches, marks the current one, and refreshes the tree on a switch", async () => {
    const onTreeChanged = vi.fn();
    const { port } = draw({ changeCount: 2, onTreeChanged });
    const chip = await screen.findByText("main");
    fireEvent.click(chip.closest("button")!);
    const menu = await screen.findByRole("menu");
    const marked = menu.querySelector("[data-on]");
    expect(marked?.getAttribute("data-value")).toBe("main");
    expect(screen.getByRole("menuitem", { name: "studio" })).toBeTruthy();
    fireEvent.click(screen.getByRole("menuitem", { name: "studio" }));
    await waitFor(() => expect(onTreeChanged).toHaveBeenCalled());
    expect(port.branchState.branch).toBe("studio");
  });

  it("re-reads the branch list each time the menu opens", async () => {
    const { port } = draw({});
    const chip = await screen.findByText("main");
    const spy = vi.spyOn(port, "branches");
    fireEvent.click(chip.closest("button")!);
    await screen.findByRole("menu");
    fireEvent.keyDown(document.body, { key: "Escape" });
    fireEvent.click(chip.closest("button")!);
    await screen.findByRole("menu");
    expect(spy).toHaveBeenCalledTimes(2);
  });

  it("dismisses completion when focus moves to a toolbar control", async () => {
    const { box } = draw();
    fireEvent.change(box, { target: { value: "@", selectionStart: 1 } });
    expect(await screen.findByRole("listbox", { name: "补全" })).toBeTruthy();
    fireEvent.blur(box);
    await waitFor(() => expect(screen.queryByRole("listbox", { name: "补全" })).toBeNull());
  });

  it("gives each composer its own completion ownership ids", async () => {
    const first = draw();
    const second = draw();
    fireEvent.change(first.box, { target: { value: "/", selectionStart: 1 } });
    fireEvent.change(second.box, { target: { value: "/", selectionStart: 1 } });
    await waitFor(() => expect(screen.getAllByRole("listbox", { name: "补全" })).toHaveLength(2));
    const ids = screen.getAllByRole("listbox", { name: "补全" }).map((node) => node.id);
    expect(new Set(ids).size).toBe(2);
    expect(first.box.getAttribute("aria-controls")).toBe(ids[0]);
    expect(second.box.getAttribute("aria-controls")).toBe(ids[1]);
  });
});

describe("composer attachments", () => {
  it("settles files independently and keeps a failed item actionable", async () => {
    const port = new MockPort();
    const attach = vi.fn(async (_blob: Blob, name: string): Promise<Attachment> => {
      if (name === "bad.txt") throw new Error("too large");
      return { path: `.reasonix/attachments/${name}`, ref: `@.reasonix/attachments/${name}`, image: false };
    });
    (port as unknown as { attach: typeof attach }).attach = attach;
    const { container } = draw({ port });
    const input = container.querySelector('input[type="file"]') as HTMLInputElement;
    fireEvent.change(input, {
      target: { files: [new File(["bad"], "bad.txt", { type: "text/plain" }), new File(["ok"], "good.txt", { type: "text/plain" })] },
    });
    expect(await screen.findByText("good.txt")).toBeTruthy();
    expect(await screen.findByRole("button", { name: "添加失败 · 重试" })).toBeTruthy();
    expect(screen.getByText("too large")).toBeTruthy();
    expect(screen.getByRole("button", { name: "移除 bad.txt" })).toBeTruthy();
    expect((screen.getByRole("button", { name: "发送" }) as HTMLButtonElement).disabled).toBe(true);
  });

  it("shows why an attachment failed on the chip, in the window's language, and retries it", async () => {
    const port = new MockPort();
    const attach = vi
      .fn<(blob: Blob, name: string) => Promise<Attachment>>()
      .mockRejectedValueOnce(
        new HttpError(415, "pasted data is not a supported image", {
          code: "attachment.unsupported_image",
          params: { format: ".ico", supported: "PNG, JPEG, GIF, WebP" },
        }),
      )
      .mockResolvedValue({ path: ".reasonix/attachments/a.png", ref: "@.reasonix/attachments/a.png", image: true });
    (port as unknown as { attach: typeof attach }).attach = attach;
    const { container } = draw({ port });
    const input = container.querySelector('input[type="file"]') as HTMLInputElement;
    fireEvent.change(input, { target: { files: [new File(["x"], "favicon.ico", { type: "image/x-icon" })] } });
    const why = await screen.findByText("这个文件的格式暂不支持（.ico）。支持的图片格式：PNG, JPEG, GIF, WebP。可以先转换格式再添加。");
    expect(why.closest("li")?.querySelector("button.retry")).toBeTruthy();
    expect((screen.getByRole("button", { name: "发送" }) as HTMLButtonElement).disabled).toBe(true);
    fireEvent.click(screen.getByRole("button", { name: "添加失败 · 重试" }));
    await waitFor(() => expect(screen.queryByText(/格式暂不支持/)).toBeNull());
    expect(attach).toHaveBeenCalledTimes(2);
  });

  it("keeps image routing guidance outside the horizontal attachment rail", async () => {
    const port = new MockPort();
    vi.spyOn(port, "attach").mockResolvedValue({ path: ".reasonix/attachments/shot.png", ref: "@shot.png", image: true });
    const { container } = draw({ port, st: status({ vision: false, visionDeclared: false }) });
    const input = container.querySelector('input[type="file"]') as HTMLInputElement;
    fireEvent.change(input, { target: { files: [new File(["png"], "shot.png", { type: "image/png" })] } });
    const warning = await waitFor(() => {
      const element = document.querySelector<HTMLElement>(".shotwarn");
      if (!element) throw new Error("image routing guidance not mounted");
      return element;
    });
    expect(warning.className).toBe("shotwarn");
    expect(warning.closest(".shots")).toBeNull();
  });
});

it("keeps every segment after the provider in a namespaced model id", async () => {
  const { container } = draw({ st: status({ modelRef: "relay/anthropic/claude-sonnet" }) });
  // The rows are the ready signal, and a Picker renders them into the body.
  await waitFor(() => expect(document.querySelector('[data-action="model.select"]')).toBeTruthy());
  expect(container.querySelector(".studio-model-group .model-picker")?.textContent).toContain("anthropic/claude-sonnet");
});

describe("the effort ladder follows the source", () => {
  const relay = (efforts?: string[]): ModelEntry[] => [{ ref: "relay/model-x", provider: "relay", model: "model-x", efforts }];
  const onRelay = status({ modelRef: "relay/model-x" });
  const trigger = (c: HTMLElement) => c.querySelector(".studio-effort-picker") as HTMLElement;

  it("re-reads the ladder when settings report a change", async () => {
    const port = new MockPort();
    const models = vi.spyOn(port, "models").mockResolvedValueOnce(relay()).mockResolvedValue(relay(["auto", "low", "high"]));
    const props = { port: port as unknown as AgentPort, status: onRelay, running: false, focus: 0,
      onSubmit: vi.fn(async () => true), onChanged: vi.fn(), onError: vi.fn() };
    const view = render(<Composer {...props} pulse={0} />);
    await waitFor(() => expect(trigger(view.container).textContent).toContain("未声明"));

    view.rerender(<Composer {...props} pulse={1} />);
    await waitFor(() => expect(trigger(view.container).textContent).not.toContain("未声明"));
    expect(models).toHaveBeenCalledTimes(2);
  });

  it("re-reads the ladder when its menu opens", async () => {
    const port = new MockPort();
    const models = vi.spyOn(port, "models").mockResolvedValueOnce(relay()).mockResolvedValue(relay(["auto", "low"]));
    const { container } = draw({ port, st: onRelay });
    await waitFor(() => expect(trigger(container).textContent).toContain("未声明"));

    fireEvent.click(trigger(container));
    await waitFor(() => expect(trigger(container).textContent).not.toContain("未声明"));
    expect(models).toHaveBeenCalledTimes(2);
  });
});

describe("a model mode switch", () => {
  const gpt: ModelEntry[] = [{ ref: "openai/gpt-5.6-sol", provider: "openai", model: "gpt-5.6-sol", efforts: ["auto", "medium", "high"] }];
  const pro = (active: boolean) => [{ id: "pro", labelKey: "model_mode.pro", hintKey: "model_mode.pro.hint", costlier: true, active }];
  const open = async (st: SessionStatus, port = new MockPort()) => {
    vi.spyOn(port, "models").mockResolvedValue(gpt);
    const view = draw({ port, st });
    await waitFor(() => expect(view.container.querySelector(".studio-effort-picker")?.textContent).not.toContain("未声明"));
    fireEvent.click(view.container.querySelector(".studio-effort-picker") as HTMLElement);
    return { ...view, port };
  };

  it("is drawn only for a model that declares a mode", async () => {
    await open(status({ modelRef: "openai/gpt-5.6-sol" }));
    expect(screen.queryByRole("menuitemcheckbox")).toBeNull();
  });

  it("says it costs more and turns the mode on", async () => {
    const port = new MockPort();
    const set = vi.spyOn(port, "setModelMode");
    await open(status({ modelRef: "openai/gpt-5.6-sol", modes: pro(false) }), port);
    const row = screen.getByRole("menuitemcheckbox");
    expect(row.getAttribute("aria-checked")).toBe("false");
    expect(row.textContent).toContain("Pro 模式");
    expect(row.textContent).toContain("费用更高");
    fireEvent.click(row);
    await waitFor(() => expect(set).toHaveBeenCalledWith("pro"));
  });

  it("reads as on, and a second pick turns it off", async () => {
    const port = new MockPort();
    const set = vi.spyOn(port, "setModelMode");
    const { container } = await open(status({ modelRef: "openai/gpt-5.6-sol", modes: pro(true) }), port);
    expect(container.querySelector(".studio-effort-picker")?.textContent).toContain("Pro 模式");
    const row = screen.getByRole("menuitemcheckbox");
    expect(row.getAttribute("aria-checked")).toBe("true");
    fireEvent.click(row);
    await waitFor(() => expect(set).toHaveBeenCalledWith(""));
  });
});

describe("a line taken back from the queue", () => {
  const props = () => ({ port: new MockPort() as unknown as AgentPort, status: status(), running: false, onSubmit: async () => true, onChanged: vi.fn(), onError: vi.fn() });

  it("lands in the box", () => {
    const view = render(<Composer {...props()} />);
    view.rerender(<Composer {...props()} restore={{ n: 1, text: "取回的这一句" }} />);
    expect((view.container.querySelector("textarea") as HTMLTextAreaElement).value).toBe("取回的这一句");
  });

  it("goes under what was typed meanwhile instead of replacing it", () => {
    const p = props();
    const view = render(<Composer {...p} />);
    const box = view.container.querySelector("textarea") as HTMLTextAreaElement;
    fireEvent.change(box, { target: { value: "正在写的", selectionStart: 4 } });
    view.rerender(<Composer {...p} restore={{ n: 1, text: "取回的这一句" }} />);
    expect(box.value).toBe("正在写的\n取回的这一句");
  });

  it("restores the same text twice when it is taken back twice", () => {
    const p = props();
    const view = render(<Composer {...p} />);
    view.rerender(<Composer {...p} restore={{ n: 1, text: "甲" }} />);
    const box = view.container.querySelector("textarea") as HTMLTextAreaElement;
    fireEvent.change(box, { target: { value: "", selectionStart: 0 } });
    view.rerender(<Composer {...p} restore={{ n: 2, text: "甲" }} />);
    expect(box.value).toBe("甲");
  });
});


describe("send shortcut preference", () => {
  it("keeps Enter for multiline drafting and sends with Control+Enter", async () => {
    localStorage.setItem("rx-send-shortcut", "modifier_enter");
    const { box, onSubmit } = draw();
    fireEvent.change(box, { target: { value: "第一行\n第二行", selectionStart: 7 } });
    expect(fireEvent.keyDown(box, { key: "Enter" })).toBe(true);
    expect(onSubmit).not.toHaveBeenCalled();
    expect(box.getAttribute("aria-keyshortcuts")).toBe("Control+ENTER");
    expect(screen.getByText("Ctrl Enter 发送 · Enter 换行")).toBeTruthy();
    fireEvent.keyDown(box, { key: "Enter", ctrlKey: true });
    await waitFor(() => expect(onSubmit).toHaveBeenCalledWith("第一行\n第二行"));
  });

  it("never sends an IME confirmation in modifier mode", async () => {
    localStorage.setItem("rx-send-shortcut", "modifier_enter");
    const { box, onSubmit } = draw();
    fireEvent.change(box, { target: { value: "中文候选", selectionStart: 4 } });
    fireEvent.compositionStart(box);
    fireEvent.keyDown(box, { key: "Enter", ctrlKey: true, isComposing: true });
    fireEvent.compositionEnd(box);
    fireEvent.keyDown(box, { key: "Enter", ctrlKey: true });
    expect(onSubmit).not.toHaveBeenCalled();
    await act(async () => { await new Promise((resolve) => setTimeout(resolve, 120)); });
    fireEvent.keyDown(box, { key: "Enter" });
    expect(onSubmit).not.toHaveBeenCalled();
    fireEvent.keyDown(box, { key: "Enter", ctrlKey: true });
    await waitFor(() => expect(onSubmit).toHaveBeenCalledWith("中文候选"));
  });

  it("preserves Alt+Enter submission in Enter mode", async () => {
    const { box, onSubmit } = draw();
    fireEvent.change(box, { target: { value: "兼容已有快捷键", selectionStart: 7 } });
    fireEvent.keyDown(box, { key: "Enter", altKey: true });
    await waitFor(() => expect(onSubmit).toHaveBeenCalledWith("兼容已有快捷键"));
  });

  it.each([false, true])("uses tap hints on touch screens in modifier mode (running=%s)", (running) => {
    localStorage.setItem("rx-send-shortcut", "modifier_enter");
    touchPointer();
    draw({ running });
    expect(screen.getByText(running ? "点按插话 · 回车换行" : "点按发送 · 回车换行")).toBeTruthy();
  });

  it.each(["Enter", "Tab", "Control+Enter"])("routes %s correctly while a slash completion owns Enter", async (key) => {
    localStorage.setItem("rx-send-shortcut", "modifier_enter");
    const port = new MockPort();
    vi.spyOn(port, "complete").mockResolvedValue({ kind: "slash", from: 0, to: 4, query: "/rev", items: [{ label: "/review", insert: "/review ", kind: "skill" }] });
    const { box, onSubmit } = draw({ port });
    fireEvent.change(box, { target: { value: "/rev", selectionStart: 4 } });
    await screen.findByRole("option", { name: /review/ });
    fireEvent.keyDown(box, { key: key === "Tab" ? "Tab" : "Enter", ctrlKey: key === "Control+Enter" });
    if (key === "Control+Enter") await waitFor(() => expect(onSubmit).toHaveBeenCalledWith("/rev"));
    else { expect(box.value).toBe("/review "); expect(onSubmit).not.toHaveBeenCalled(); }
  });

  it("keeps the modifier-to-steer hint while a turn runs", () => {
    localStorage.setItem("rx-send-shortcut", "modifier_enter");
    const { box, onSubmit } = draw({ running: true });
    fireEvent.change(box, { target: { value: "补充一句", selectionStart: 4 } });
    fireEvent.keyDown(box, { key: "Enter" });
    expect(onSubmit).not.toHaveBeenCalled();
    expect(screen.getByText("Ctrl Enter 插话 · Enter 换行")).toBeTruthy();
  });
});
