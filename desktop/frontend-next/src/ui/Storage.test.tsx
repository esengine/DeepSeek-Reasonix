// @vitest-environment jsdom
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import "./testkit";
import type { AgentPort } from "../port/port";
import type { HubPort, LegacySkip } from "../port/hub";
import type { StorageQuery, StorageRoot, StorageState } from "../port/storage";
import { Storage } from "./Storage";

beforeEach(() => vi.useFakeTimers());
afterEach(() => {
  cleanup();
  vi.useRealTimers();
});

const root = (id: string, over: Partial<StorageRoot> = {}): StorageRoot => ({
  id, dir: `/data/${id}`, bytes: 0, files: 0, relocatable: false, ...over,
});
const state = (roots: StorageRoot[]): StorageState => ({ roots, editable: false });

function draw(answer: (q?: StorageQuery) => Promise<StorageState>) {
  const port = { storage: vi.fn(answer) } as unknown as AgentPort;
  render(<Storage port={port} hub={{} as HubPort} workspace="" onRecovered={() => {}} />);
  return port;
}

const flush = () => act(async () => { await vi.advanceTimersByTimeAsync(0); });

it("shows fast roots while a slow one is still being measured", async () => {
  draw(async (q) => {
    if (q?.layout) return state([root("state", { pending: true }), root("cache", { pending: true })]);
    if (q?.root === "cache") return state([root("cache", { bytes: 2048, files: 4 })]);
    return new Promise(() => {});
  });
  await flush();
  expect(screen.getByText(/2(\.0)? KB · 4 个文件/)).toBeTruthy();
  expect(screen.getAllByText("正在统计…")).toHaveLength(1);
});

it("says a root timed out instead of spinning forever", async () => {
  draw(async (q) => {
    if (q?.layout) return state([root("state", { pending: true })]);
    return new Promise(() => {});
  });
  await flush();
  expect(screen.getByText("正在统计…")).toBeTruthy();
  await act(async () => { await vi.advanceTimersByTimeAsync(21_000); });
  expect(screen.getByText("统计超时")).toBeTruthy();
  expect(screen.queryByText("正在统计…")).toBeNull();
});

it("marks a truncated count as a lower bound", async () => {
  draw(async (q) => {
    if (q?.layout) return state([root("state", { pending: true })]);
    return state([root("state", { bytes: 3072, files: 9, truncated: true })]);
  });
  await flush();
  expect(screen.getByText(/^≥ 3(\.0)? KB · 9 个文件/)).toBeTruthy();
  expect(screen.getByText("目录过大，已统计到时间上限，实际占用不小于此数")).toBeTruthy();
});

function recover(answer: { imported: number; warnings: number; recognised: boolean; skipped?: LegacySkip[] }, picked: string | null = "/old") {
  const port = { storage: vi.fn(async () => state([])) } as unknown as AgentPort;
  const hub = {
    pickFolder: vi.fn(async () => picked),
    importLegacySessions: vi.fn(async () => ({ summary: "", skipped: [], ...answer })),
  } as unknown as HubPort;
  render(<Storage port={port} hub={hub} workspace="/ws" onRecovered={() => {}} />);
  return hub;
}

const pickOld = async () => {
  await flush();
  fireEvent.click(screen.getByText("选择旧版数据目录…"));
  await flush();
};

it("asks for the path in words when the folder panel could not be shown, and imports from it", async () => {
  const hub = recover({ imported: 2, warnings: 0, recognised: true }, null);
  await pickOld();
  expect(hub.importLegacySessions).not.toHaveBeenCalled();
  fireEvent.change(screen.getByLabelText("旧版数据目录路径"), { target: { value: "/home/a/.reasonix" } });
  fireEvent.click(screen.getByText("导入此文件夹"));
  await flush();
  expect(hub.importLegacySessions).toHaveBeenCalledWith("/home/a/.reasonix", "/ws");
  expect(screen.getByText(/已找回 2 个会话。/)).toBeTruthy();
});

it("does not ask for a path when the person closed the folder panel", async () => {
  const hub = recover({ imported: 0, warnings: 0, recognised: true }, "");
  await pickOld();
  expect(hub.importLegacySessions).not.toHaveBeenCalled();
  expect(screen.queryByLabelText("旧版数据目录路径")).toBeNull();
});

it("says the folder holds no 1.x sessions when it was not recognised", async () => {
  recover({ imported: 0, warnings: 0, recognised: false });
  await pickOld();
  expect(screen.getByText(/这个文件夹里没有找到旧版会话/)).toBeTruthy();
  expect(screen.queryByText("没有发现尚未导入的旧会话。")).toBeNull();
});

it("says nothing new when a recognised folder was already imported", async () => {
  recover({ imported: 0, warnings: 0, recognised: true });
  await pickOld();
  expect(screen.getByText("没有发现尚未导入的旧会话。")).toBeTruthy();
  expect(screen.queryByText(/这个文件夹里没有找到旧版会话/)).toBeNull();
});

it("reports the imported count together with the warnings", async () => {
  recover({ imported: 3, warnings: 2, recognised: true });
  await pickOld();
  expect(screen.getByText(/已找回 3 个会话。.*有 2 项无法读取。/)).toBeTruthy();
});

const skip = (name: string, reason: LegacySkip["reason"]): LegacySkip => ({ source: "/old/sessions-v4", name, path: `/old/sessions-v4/${name}`, reason });

it("lists each session it could not import with its reason, path and that the file is untouched", async () => {
  recover({
    imported: 1,
    warnings: 3,
    recognised: true,
    skipped: [skip("aaa", "too_large"), skip("bbb", "schema_unsupported"), skip("ccc", "corrupt")],
  });
  await pickOld();
  expect(screen.getByText("有 3 个会话没有导入")).toBeTruthy();
  expect(screen.getByText(/这些文件仍在原位置，没有被移动或删除/)).toBeTruthy();
  for (const [name, reason] of [["aaa", "体积超过读取上限"], ["bbb", "来自本版本尚不支持的存储版本"], ["ccc", "文件已损坏"]]) {
    const row = screen.getByText(name).closest(".item") as HTMLElement;
    expect(within(row).getByText(reason)).toBeTruthy();
    expect(within(row).getByText(`/old/sessions-v4/${name}`)).toBeTruthy();
  }
});

it("copies the path of a skipped session", async () => {
  const writeText = vi.fn(async () => {});
  Object.defineProperty(navigator, "clipboard", { value: { writeText }, configurable: true });
  recover({ imported: 0, warnings: 1, recognised: true, skipped: [skip("aaa", "permission")] });
  await pickOld();
  fireEvent.click(screen.getByLabelText("复制路径：aaa"));
  expect(writeText).toHaveBeenCalledWith("/old/sessions-v4/aaa");
});

it("names the session on every copy button", async () => {
  recover({ imported: 0, warnings: 2, recognised: true, skipped: [skip("aaa", "corrupt"), skip("bbb", "corrupt")] });
  await pickOld();
  expect(screen.getByLabelText("复制路径：aaa")).toBeTruthy();
  expect(screen.getByLabelText("复制路径：bbb")).toBeTruthy();
});

it("draws no list when nothing was skipped", async () => {
  recover({ imported: 2, warnings: 0, recognised: true });
  await pickOld();
  expect(screen.queryByText(/个会话没有导入/)).toBeNull();
});

function movable(plan: (root: string, dir: string) => Promise<{ ok: boolean; root: string; from: string; to: string; bytes: number; files: number; free: number; need: number; refusals: [] }>) {
  const port = {
    storage: vi.fn(async () => ({ roots: [root("state", { relocatable: true, bytes: 10, files: 1 })], editable: true })),
    planStorageMove: vi.fn(plan),
  } as unknown as AgentPort;
  render(<Storage port={port} hub={{} as HubPort} workspace="" onRecovered={() => {}} />);
  return port;
}

const answered = async (_root: string, dir: string) => ({ ok: true, root: "state", from: "/a", to: dir, bytes: 10, files: 1, free: 1e9, need: 1e6, refusals: [] as [] });

it("asks the kernel to plan a move once the path stops changing, not once per keystroke", async () => {
  const port = movable(answered);
  await flush();
  fireEvent.click(screen.getByText("移动…"));
  const input = screen.getByRole("textbox") as HTMLInputElement;
  for (const typed of ["D", "D:", "D:\\", "D:\\t", "D:\\te"]) fireEvent.change(input, { target: { value: typed } });
  expect(port.planStorageMove).not.toHaveBeenCalled();
  await act(async () => { await vi.advanceTimersByTimeAsync(600); });
  expect(port.planStorageMove).toHaveBeenCalledTimes(1);
  expect(port.planStorageMove).toHaveBeenCalledWith("state", "D:\\te");
});

it("drops an answer for a path the person has already typed past", async () => {
  let release: (v: Awaited<ReturnType<typeof answered>>) => void = () => {};
  movable((r, d) =>
    d === "/slow" ? new Promise((res) => { release = res; }) : answered(r, d).then((p) => ({ ...p, ok: false })),
  );
  await flush();
  fireEvent.click(screen.getByText("移动…"));
  const input = screen.getByRole("textbox") as HTMLInputElement;
  fireEvent.change(input, { target: { value: "/slow" } });
  await act(async () => { await vi.advanceTimersByTimeAsync(600); });
  fireEvent.change(input, { target: { value: "/fast" } });
  await act(async () => { await vi.advanceTimersByTimeAsync(600); });
  await act(async () => { release(await answered("state", "/slow")); });
  const start = screen.getByRole("button", { name: /开始迁移/ }) as HTMLButtonElement;
  expect(start.disabled).toBe(true);
});
