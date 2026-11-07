// @vitest-environment jsdom
import { Profiler, type PropsWithChildren } from "react";
import { act, cleanup, render, renderHook, screen } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { MockPort } from "../port/mock";
import type { WorkspaceFile } from "../port/port";
import { useWorkbenchFiles } from "./useWorkbenchFiles";
import { WorkbenchPanel } from "./WorkbenchPanel";
import userEvent from "@testing-library/user-event";
import "./testkit";

beforeEach(() => vi.useFakeTimers({ toFake: ["setInterval", "clearInterval"] }));
afterEach(() => { cleanup(); vi.useRealTimers(); });
const tick = () => act(async () => { await vi.advanceTimersByTimeAsync(3000); });

async function mount(path = "notes.md") {
  const port = new MockPort();
  const file = { path, content: "# Saved", revision: "r1" };
  const read = vi.spyOn(port, "workspaceFile").mockImplementation(async () => ({ ...file }));
  const commits = vi.fn();
  const openPaths = [path];
  const wrapper = ({ children }: PropsWithChildren) => <Profiler id="files" onRender={commits}>{children}</Profiler>;
  const view = renderHook(({ refresh }) => useWorkbenchFiles({ port, workspaceRoot: "/fixture", path, openPaths, shown: true, refresh, poll: true }), { initialProps: { refresh: "" }, wrapper });
  await act(async () => {});
  expect(view.result.current.file).toEqual(file);
  expect(view.result.current.busy).toBe(false);
  commits.mockClear();
  return { ...view, file, read, commits };
}

it.each(["notes.md", "index.html"])("does not commit unchanged quiet polls for %s", async (path) => {
  const f = await mount(path);
  await tick();
  await tick();
  expect(f.read).toHaveBeenCalledTimes(3);
  expect(f.commits).not.toHaveBeenCalled();
  expect(f.result.current.draft).toBe(f.file.content);
});

it("keeps loaded pending and failed quiet polls out of React commits", async () => {
  const f = await mount();
  let reject!: (error: Error) => void;
  f.read.mockReturnValueOnce(new Promise((_, fail) => { reject = fail; }));
  await tick();
  await tick();
  expect(f.read).toHaveBeenCalledTimes(2);
  expect(f.commits).not.toHaveBeenCalled();
  expect(f.result.current.busy).toBe(false);
  await act(async () => reject(new Error("atomic replacement")));
  expect(f.commits).not.toHaveBeenCalled();
  expect(f.result.current.error).toBe("");
  await tick();
  expect(f.read).toHaveBeenCalledTimes(3);
  expect(f.commits).not.toHaveBeenCalled();
});

it.each([{ content: "# Changed" }, { revision: "r2" }])("commits changed visible file state: %j", async (change) => {
  const f = await mount();
  f.read.mockResolvedValue({ ...f.file, ...change });
  await tick();
  expect(f.result.current.file).toEqual({ ...f.file, ...change });
  expect(f.result.current.draft).toBe(change.content ?? f.file.content);
  expect(f.commits).toHaveBeenCalledTimes(1);
});

it("commits a cleared visible error even when content and revision are unchanged", async () => {
  const f = await mount();
  f.read.mockRejectedValueOnce(new Error("read refused"));
  await act(async () => f.rerender({ refresh: "changed" }));
  expect(f.result.current.error).toBe("read refused");
  f.commits.mockClear();
  await tick();
  expect(f.result.current.error).toBe("");
  expect(f.commits).toHaveBeenCalledTimes(1);
});

it("does not commit a discarded quiet read after a draft edit", async () => {
  const f = await mount();
  let resolve!: (file: WorkspaceFile) => void;
  f.read.mockReturnValueOnce(new Promise((done) => { resolve = done; }));
  await tick();
  act(() => f.result.current.setDraft("# Unsaved"));
  f.commits.mockClear();
  await act(async () => resolve({ ...f.file, content: "# External", revision: "r2" }));
  expect(f.result.current.draft).toBe("# Unsaved");
  expect(f.commits).not.toHaveBeenCalled();
});

it("leaves the rendered Workbench untouched when Markdown polls are unchanged", async () => {
  const user = userEvent.setup();
  const port = new MockPort();
  vi.spyOn(port, "workspaceFiles").mockResolvedValue({ files: ["notes.md"], directories: [] });
  const read = vi.spyOn(port, "workspaceFile").mockResolvedValue({ path: "notes.md", content: "# Saved", revision: "r1" });
  const commits = vi.fn();
  render(<Profiler id="workbench" onRender={commits}><WorkbenchPanel port={port} workspaceRoot="/fixture"
    tabs={[]} manual={false} shown scheme="light" changes={[]}
    onCloseManual={() => {}} onSurfaces={() => {}} onExternal={() => {}} /></Profiler>);
  await user.click(await screen.findByRole("button", { name: "notes.md" }));
  await screen.findByRole("heading", { name: "Saved" });
  await act(async () => {});
  // A tab that has just opened clears its entering state on a real 220ms timer
  // (the fake clock covers intervals only), so the baseline waits it out.
  await act(async () => { await new Promise((resolve) => setTimeout(resolve, 300)); });
  commits.mockClear();
  await tick();
  await tick();
  expect(read).toHaveBeenCalledTimes(3);
  expect(commits).not.toHaveBeenCalled();
});
