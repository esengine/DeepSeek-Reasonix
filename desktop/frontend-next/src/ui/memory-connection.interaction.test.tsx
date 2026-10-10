// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { Memory } from "./Memory";
import { Settings } from "./Settings";
import { MockPort } from "../port/mock";
import { MockHub } from "../port/mock_hub";
import type { AgentPort, MemoryCatalog, MemoryEntry } from "../port/port";

afterEach(cleanup);

function pending<T>() {
  let resolve!: (value: T) => void;
  let reject!: (error: Error) => void;
  const promise = new Promise<T>((ok, fail) => { resolve = ok; reject = fail; });
  return { promise, resolve, reject };
}

function fixture(label: string, revision = 2, name = "shared-note") {
  const port = new MockPort() as unknown as AgentPort;
  const current: MemoryEntry = { name, title: label, body: `${label} body`, description: `${label} description`,
    activation: "relevant", scope: "project", revision };
  const older = { ...current, title: `${label} older`, body: `${label} older body`, revision: revision - 1 };
  const catalog: MemoryCatalog = { memories: [current], recallQuery: `${label} query` };
  const read = vi.spyOn(port, "memories").mockResolvedValue(catalog);
  const history = vi.spyOn(port, "memoryRevisions").mockResolvedValue([current, older]);
  return { port, current, older, catalog, read, history };
}

async function open(label: string) {
  await userEvent.click(await screen.findByRole("button", { name: label }));
}

async function edit(label: string, body: string) {
  await open(label);
  await userEvent.click(screen.getByRole("button", { name: "编辑" }));
  fireEvent.change(screen.getByRole("textbox", { name: "正文" }), { target: { value: body } });
}

async function history(label: string, revision: number) {
  await screen.findByRole("button", { name: label });
  const name = `第 ${revision} 版，查看历史`;
  if (!screen.queryByRole("button", { name })) await open(label);
  await userEvent.click(screen.getByRole("button", { name }));
}

it.each(["success", "failure"])("ignores an old catalog %s after the new connection has loaded", async (outcome) => {
  const old = fixture("old");
  const next = fixture("next");
  const read = pending<MemoryCatalog>();
  old.read.mockReturnValue(read.promise);
  const view = render(<Memory port={old.port} />);
  view.rerender(<Memory port={next.port} />);
  await screen.findByRole("button", { name: "next" });
  await act(async () => {
    if (outcome === "success") read.resolve(old.catalog);
    else read.reject(new Error("old read failed"));
  });
  expect(screen.getByRole("button", { name: "next" })).toBeTruthy();
  expect(screen.queryByRole("button", { name: "old" })).toBeNull();
  expect(screen.queryByText("old read failed")).toBeNull();
  expect(document.querySelector(".recall")?.textContent).toContain("next query");
});

it.each(["save", "forget", "restore"])("cannot route an old %s control to a pending new connection", async (action) => {
  const old = fixture("old");
  const next = fixture("next", 7);
  const read = pending<MemoryCatalog>();
  next.read.mockReturnValue(read.promise);
  const save = vi.spyOn(next.port, "saveMemory").mockResolvedValue();
  const forget = vi.spyOn(next.port, "forgetMemory").mockResolvedValue();
  const restore = vi.spyOn(next.port, "restoreMemory").mockResolvedValue();
  const view = render(<Memory port={old.port} />);
  if (action === "save") await edit("old", "old edited body");
  else if (action === "restore") {
    await history("old", 2);
    await screen.findByText("old older body");
  } else await screen.findByRole("button", { name: "忘记" });
  view.rerender(<Memory port={next.port} />);
  const label = action === "save" ? "保存" : action === "restore" ? "恢复此版本" : "忘记";
  const staleControl = screen.queryByRole("button", { name: label });
  if (staleControl) await userEvent.click(staleControl);
  expect(save).not.toHaveBeenCalled();
  expect(forget).not.toHaveBeenCalled();
  expect(restore).not.toHaveBeenCalled();
  expect(screen.getByText("正在读取记忆…")).toBeTruthy();
  expect(screen.queryByRole("button", { name: "old" })).toBeNull();
  await act(async () => read.resolve(next.catalog));
  await screen.findByRole("button", { name: "next" });
});

it("starts a fresh same-name editor after switching connections", async () => {
  const old = fixture("old");
  const next = fixture("next");
  const save = vi.spyOn(next.port, "saveMemory").mockResolvedValue();
  const view = render(<Memory port={old.port} />);
  await edit("old", "old draft");
  view.rerender(<Memory port={next.port} />);
  await screen.findByRole("button", { name: "next" });
  expect(screen.queryByRole("textbox", { name: "正文" })).toBeNull();
  await edit("next", "next draft");
  await userEvent.click(screen.getByRole("button", { name: "保存" }));
  expect(save).toHaveBeenCalledExactlyOnceWith({ name: next.current.name, title: "next", description: "next description",
    body: "next draft", activation: "relevant" });
});

it("reads the new connection's history instead of restoring a cached same-name revision", async () => {
  const old = fixture("old");
  const next = fixture("next", 7);
  const restore = vi.spyOn(next.port, "restoreMemory").mockResolvedValue();
  const view = render(<Memory port={old.port} />);
  await history("old", 2);
  await screen.findByText("old older body");
  view.rerender(<Memory port={next.port} />);
  await screen.findByRole("button", { name: "next" });
  expect(screen.queryByText("old older body")).toBeNull();
  await history("next", 7);
  await screen.findByText("next older body");
  await userEvent.click(screen.getByRole("button", { name: "恢复此版本" }));
  expect(next.history).toHaveBeenCalledExactlyOnceWith(next.current.name);
  expect(restore).toHaveBeenCalledExactlyOnceWith(next.current.name, 6);
});

it.each(["success", "failure"])("ignores a history %s from the previous connection while the new history is pending", async (outcome) => {
  const old = fixture("old");
  const next = fixture("next", 7);
  const oldHistory = pending<MemoryEntry[]>();
  const newHistory = pending<MemoryEntry[]>();
  old.history.mockReturnValue(oldHistory.promise);
  next.history.mockReturnValue(newHistory.promise);
  const view = render(<Memory port={old.port} />);
  await history("old", 2);
  view.rerender(<Memory port={next.port} />);
  await screen.findByRole("button", { name: "next" });
  const expanded = screen.queryByRole("button", { name: "收起旧版本" });
  if (expanded) await userEvent.click(expanded);
  await history("next", 7);
  await act(async () => {
    if (outcome === "success") oldHistory.resolve([old.current, old.older]);
    else oldHistory.reject(new Error("old history failed"));
  });
  expect(screen.getByText("正在读取历史版本…")).toBeTruthy();
  expect(screen.queryByText("old history failed")).toBeNull();
  expect(screen.queryByText("old older body")).toBeNull();
  await act(async () => newHistory.resolve([next.current, next.older]));
  expect(screen.getByText("next older body")).toBeTruthy();
});

it.each(["success", "failure"])("does not reuse a catalog from an earlier lifetime of the same port (%s)", async (outcome) => {
  const old = fixture("old");
  const next = fixture("next");
  const read = pending<MemoryCatalog>();
  const returned = { ...old.catalog, memories: [{ ...old.current, title: "returned" }], recallQuery: "returned query" };
  old.read.mockReturnValueOnce(read.promise).mockResolvedValue(returned);
  const view = render(<Memory port={old.port} />);
  view.rerender(<Memory port={next.port} />);
  await screen.findByRole("button", { name: "next" });
  view.rerender(<Memory port={old.port} />);
  await screen.findByRole("button", { name: "returned" });
  await act(async () => {
    if (outcome === "success") read.resolve(old.catalog);
    else read.reject(new Error("earlier lifetime failed"));
  });
  expect(screen.getByRole("button", { name: "returned" })).toBeTruthy();
  expect(screen.queryByText("earlier lifetime failed")).toBeNull();
  expect(old.read).toHaveBeenCalledTimes(2);
});

it.each(["save", "restore", "forget"])("keeps an old %s completion out of the new editor and pending save", async (action) => {
  const old = fixture("old");
  const next = fixture("next", 7, "new-note");
  const oldWrite = pending<void>();
  const newWrite = pending<void>();
  const oldSave = vi.spyOn(old.port, "saveMemory").mockReturnValue(oldWrite.promise);
  const oldRestore = vi.spyOn(old.port, "restoreMemory").mockReturnValue(oldWrite.promise);
  const oldForget = vi.spyOn(old.port, "forgetMemory").mockReturnValue(oldWrite.promise);
  const newSave = vi.spyOn(next.port, "saveMemory").mockReturnValue(newWrite.promise);
  const view = render(<Memory port={old.port} />);
  if (action === "save") { await edit("old", "old submitted"); await userEvent.click(screen.getByRole("button", { name: "保存" })); }
  else if (action === "restore") { await history("old", 2); await screen.findByText("old older body"); await userEvent.click(screen.getByRole("button", { name: "恢复此版本" })); }
  else await userEvent.click(await screen.findByRole("button", { name: "忘记" }));
  view.rerender(<Memory port={next.port} />);
  await edit("next", "next submitted");
  await userEvent.click(screen.getByRole("button", { name: "保存" }));
  expect(newSave).toHaveBeenCalledTimes(1);
  await act(async () => oldWrite.resolve());
  expect(screen.getByRole<HTMLButtonElement>("button", { name: "正在保存…" }).disabled).toBe(true);
  expect(screen.getByRole<HTMLTextAreaElement>("textbox", { name: "正文" }).value).toBe("next submitted");
  expect(next.read).toHaveBeenCalledTimes(1);
  expect(old.read).toHaveBeenCalledTimes(2);
  expect(oldSave).toHaveBeenCalledTimes(action === "save" ? 1 : 0);
  expect(oldRestore).toHaveBeenCalledTimes(action === "restore" ? 1 : 0);
  expect(oldForget).toHaveBeenCalledTimes(action === "forget" ? 1 : 0);
  await act(async () => newWrite.resolve());
  expect(next.read).toHaveBeenCalledTimes(2);
});

it.each(["save", "restore", "forget"])("keeps an old %s failure out of the new editor", async (action) => {
  const old = fixture("old");
  const next = fixture("next", 7, "new-note");
  const write = pending<void>();
  vi.spyOn(old.port, "saveMemory").mockReturnValue(write.promise);
  vi.spyOn(old.port, "restoreMemory").mockReturnValue(write.promise);
  vi.spyOn(old.port, "forgetMemory").mockReturnValue(write.promise);
  const view = render(<Memory port={old.port} />);
  if (action === "save") { await edit("old", "old submitted"); await userEvent.click(screen.getByRole("button", { name: "保存" })); }
  else if (action === "restore") { await history("old", 2); await screen.findByText("old older body"); await userEvent.click(screen.getByRole("button", { name: "恢复此版本" })); }
  else await userEvent.click(await screen.findByRole("button", { name: "忘记" }));
  view.rerender(<Memory port={next.port} />);
  await edit("next", "next draft");
  await act(async () => write.reject(new Error("old write failed")));
  expect(screen.queryByText("old write failed")).toBeNull();
  expect(screen.getByRole<HTMLTextAreaElement>("textbox", { name: "正文" }).value).toBe("next draft");
  expect(screen.getByRole<HTMLButtonElement>("button", { name: "保存" }).disabled).toBe(false);
});

it("preserves a draft through ordinary renders and a refused save before retrying the exact edit", async () => {
  const current = fixture("current");
  const write = pending<void>();
  const save = vi.spyOn(current.port, "saveMemory").mockReturnValueOnce(write.promise).mockResolvedValue();
  const view = render(<Memory port={current.port} />);
  await edit("current", "edited body");
  fireEvent.change(screen.getByRole("combobox", { name: "生效时机" }), { target: { value: "pinned" } });
  view.rerender(<Memory port={current.port} />);
  expect(current.read).toHaveBeenCalledTimes(1);
  expect(screen.getByRole<HTMLTextAreaElement>("textbox", { name: "正文" }).value).toBe("edited body");
  await userEvent.click(screen.getByRole("button", { name: "保存" }));
  await act(async () => write.reject(new Error("save refused")));
  expect(screen.getByText("save refused")).toBeTruthy();
  await userEvent.click(screen.getByRole("button", { name: "保存" }));
  const expected = { name: current.current.name, title: "current", description: "current description", body: "edited body", activation: "pinned" };
  expect(save.mock.calls).toEqual([[expected], [expected]]);
  expect(current.read).toHaveBeenCalledTimes(2);
  expect(screen.queryByRole("textbox", { name: "正文" })).toBeNull();
});

it("preserves the same connection's history cache across ordinary renders", async () => {
  const current = fixture("current");
  const view = render(<Memory port={current.port} />);
  await history("current", 2);
  await screen.findByText("current older body");
  view.rerender(<Memory port={current.port} />);
  expect(screen.getByText("current older body")).toBeTruthy();
  await userEvent.click(screen.getByRole("button", { name: "收起旧版本" }));
  await userEvent.click(screen.getByRole("button", { name: "第 2 版，查看历史" }));
  expect(current.history).toHaveBeenCalledTimes(1);
  expect(current.read).toHaveBeenCalledTimes(1);
});

it("keeps save, appended restore history and forgetting usable on one connection", async () => {
  const port = new MockPort() as unknown as AgentPort;
  const original = (await port.memories()).memories[0]!;
  const read = port.memories.bind(port);
  vi.spyOn(port, "memories").mockImplementation(async () => ({ memories: (await read()).memories.filter((m) => m.name === original.name), recallQuery: "" }));
  render(<Memory port={port} />);
  await edit(original.title!, "updated body");
  await userEvent.click(screen.getByRole("button", { name: "保存" }));
  await screen.findByText("updated body");
  await userEvent.click(screen.getByRole("button", { name: `第 ${(original.revision ?? 1) + 1} 版，查看历史` }));
  await userEvent.click(await screen.findByRole("button", { name: "恢复此版本" }));
  expect((await port.memoryRevisions(original.name))[0]!.revision).toBe((original.revision ?? 1) + 2);
  await userEvent.click(screen.getByRole("button", { name: "忘记" }));
  expect(await screen.findByText("暂无记录。")).toBeTruthy();
  expect(screen.getByText("/remember")).toBeTruthy();
});

it("clears a refused read on a new connection and retries its own catalog", async () => {
  const old = fixture("old");
  const next = fixture("next");
  old.read.mockRejectedValue(new Error("old refusal"));
  next.read.mockRejectedValueOnce(new Error("next refusal")).mockResolvedValue(next.catalog);
  const view = render(<Memory port={old.port} />);
  await screen.findByText("old refusal");
  view.rerender(<Memory port={next.port} />);
  await screen.findByText("next refusal");
  expect(screen.queryByText("old refusal")).toBeNull();
  await userEvent.click(screen.getByRole("button", { name: "重试" }));
  await screen.findByRole("button", { name: "next" });
  expect(old.read).toHaveBeenCalledTimes(1);
  expect(next.read).toHaveBeenCalledTimes(2);
});

it("resets the memory panel when Settings receives a new active connection", async () => {
  const old = fixture("old");
  const next = fixture("next");
  const read = pending<MemoryCatalog>();
  next.read.mockReturnValue(read.promise);
  const hub = new MockHub();
  const draw = (port: AgentPort) => <Settings hub={hub} port={port} status={{ preset: "balanced", toolApprovalMode: "ask" } as never}
    theme="light" onTheme={() => {}} contrast="" onContrast={() => {}} weight="" onWeight={() => {}}
    look={{} as never} onLook={() => {}} reloadThemes={() => {}} onClose={() => {}} onChanged={() => {}} onError={() => {}}
    account={null} accountUnread="" reloadAccount={() => {}} at="memory" />;
  const view = render(draw(old.port));
  await edit("old", "old settings draft");
  view.rerender(draw(next.port));
  expect(screen.queryByRole("textbox", { name: "正文" })).toBeNull();
  expect(screen.queryByRole("button", { name: "old" })).toBeNull();
  expect(screen.getByText("正在读取记忆…")).toBeTruthy();
  await act(async () => read.resolve(next.catalog));
  expect(await screen.findByRole("button", { name: "next" })).toBeTruthy();
});
