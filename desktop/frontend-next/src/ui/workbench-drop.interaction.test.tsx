// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeAll, expect, it, vi } from "vitest";
import "./testkit";
import { MockPort } from "../port/mock";
import { host } from "../port/host";
import { Composer } from "./Composer";
import { WorkbenchPanel } from "./WorkbenchPanel";
import { install } from "./filedrop";

beforeAll(install);
afterEach(() => { cleanup(); localStorage.clear(); vi.restoreAllMocks(); });

function transfer() {
  const data = new Map<string, string>();
  return {
    files: [], items: [], effectAllowed: "uninitialized", dropEffect: "none",
    get types() { return [...data.keys()]; },
    setData: (type: string, value: string) => data.set(type, value),
    getData: (type: string) => data.get(type) ?? "",
  };
}

function draw(root = "/workspace", setup?: (port: MockPort) => void) {
  const port = new MockPort();
  setup?.(port);
  const dropRefs = vi.spyOn(port, "dropRefs").mockImplementation(async (paths) => paths.map((path) => ({
    path: path.slice(root.length + 1), ref: `@${path.slice(root.length + 1)}`, image: false,
  })));
  const attach = vi.spyOn(port, "attach");
  const onSubmit = vi.fn(async () => true);
  const onError = vi.fn();
  render(<>
    <WorkbenchPanel port={port} tabs={[]} manual={false} shown scheme="light"
      workspaceRoot={root} changes={[{ path: "internal/provider/retry.go", status: "M" }]}
      onCloseManual={vi.fn()} onSurfaces={vi.fn()} onExternal={vi.fn()} />
    <Composer port={port} status={null} running={false} onSubmit={onSubmit} onChanged={vi.fn()} onError={onError} />
  </>);
  return { port, dropRefs, attach, onSubmit, onError, box: screen.getByRole("combobox", { name: "任务输入" }) as HTMLTextAreaElement };
}

function drag(row: HTMLElement, box: HTMLElement) {
  expect(row.draggable).toBe(true);
  const dataTransfer = transfer();
  fireEvent.dragStart(row, { dataTransfer });
  expect(dataTransfer.effectAllowed).toBe("copy");
  fireEvent.dragOver(box, { dataTransfer });
  expect(screen.getByText("松手 → 引用 1 个文件")).toBeTruthy();
  fireEvent.drop(box, { dataTransfer });
  fireEvent.dragEnd(row, { dataTransfer });
}

it.each(["/workspace", "C:\\work\\project", "\\\\server\\share\\project"])("references the dragged file at its source workspace (%s)", async (root) => {
  const { box, dropRefs, attach, onSubmit } = draw(root);
  fireEvent.change(box, { target: { value: "Review this" } });
  drag(await screen.findByRole("button", { name: "README.md" }), box);
  await screen.findByRole("button", { name: "移除 README.md" });
  expect(dropRefs).toHaveBeenCalledWith([`${root}/README.md`]);
  expect(attach).not.toHaveBeenCalled();
  expect(box.value).toBe("Review this");
  expect(document.activeElement).toBe(box);
  expect(screen.queryByRole("tab", { name: "README.md" })).toBeNull();
  fireEvent.click(screen.getByRole("button", { name: "发送" }));
  await waitFor(() => expect(onSubmit).toHaveBeenCalledWith("@README.md Review this"));
});

it("references a folder without expanding it", async () => {
  const { box, dropRefs } = draw();
  const row = await screen.findByRole("button", { name: "internal" });
  const expanded = row.getAttribute("aria-expanded");
  drag(row, box);
  await screen.findByRole("button", { name: "移除 internal" });
  expect(dropRefs).toHaveBeenCalledWith(["/workspace/internal"]);
  expect(row.getAttribute("aria-expanded")).toBe(expanded);
});

it("references search results and changed files through the same intake", async () => {
  const { box, dropRefs } = draw();
  await screen.findByRole("button", { name: "README.md" });
  fireEvent.change(screen.getByRole("textbox", { name: "搜索文件" }), { target: { value: "retry" } });
  const row = await waitFor(() => {
    const found = document.querySelector<HTMLElement>('.workbench-tree-row[data-target="internal/provider/retry.go"]');
    expect(found).toBeTruthy();
    return found!;
  });
  drag(row, box);
  drag(document.querySelector<HTMLElement>('[data-action-click="workbench.diff"]')!, box);
  await waitFor(() => expect(screen.getAllByRole("button", { name: "移除 retry.go" })).toHaveLength(2));
  expect(dropRefs.mock.calls).toEqual([[["/workspace/internal/provider/retry.go"]], [["/workspace/internal/provider/retry.go"]]]);
});

it("keeps the nested repository prefix when dragging changed files", async () => {
  const { box, dropRefs } = draw("/workspace", (port) => {
    port.repoRows = [{ dir: "apps/site", name: "site", branch: "main", detached: false }];
    port.repoChanges = { "apps/site": [{ path: "README.md", status: "M" }] };
  });
  const row = await waitFor(() => {
    const found = document.querySelector<HTMLElement>('[data-repo="apps/site"] [data-action-click="workbench.diff"]');
    expect(found).toBeTruthy();
    return found!;
  });
  drag(row, box);
  await screen.findByRole("button", { name: "移除 README.md" });
  expect(dropRefs).toHaveBeenCalledWith(["/workspace/apps/site/README.md"]);
});

it("clears the reference hint when a drag is cancelled", async () => {
  const { box, dropRefs } = draw();
  const row = await screen.findByRole("button", { name: "README.md" });
  const dataTransfer = transfer();
  fireEvent.dragStart(row, { dataTransfer });
  fireEvent.dragOver(box, { dataTransfer });
  expect(screen.getByText("松手 → 引用 1 个文件")).toBeTruthy();
  fireEvent.dragEnd(row, { dataTransfer });
  expect(screen.queryByText("松手 → 引用 1 个文件")).toBeNull();
  expect(dropRefs).not.toHaveBeenCalled();
});

it("surfaces a refused reference without inserting the raw path", async () => {
  const { box, dropRefs, onError } = draw();
  dropRefs.mockResolvedValue([{ error: "file no longer exists" }]);
  drag(await screen.findByRole("button", { name: "README.md" }), box);
  await waitFor(() => expect(onError).toHaveBeenCalled());
  expect(screen.queryByRole("button", { name: "移除 README.md" })).toBeNull();
  expect(box.value).toBe("");
});

it("keeps plain text drops as text", async () => {
  const { box, dropRefs } = draw();
  await screen.findByRole("button", { name: "README.md" });
  const dataTransfer = transfer();
  dataTransfer.setData("text/plain", "hello");
  fireEvent.drop(box, { dataTransfer });
  expect(box.value).toBe("hello ");
  expect(dropRefs).not.toHaveBeenCalled();
});

it("keeps native file drops working and accepts a tree drag immediately afterwards", async () => {
  const { box, dropRefs, attach } = draw();
  const row = await screen.findByRole("button", { name: "README.md" });
  vi.spyOn(Date, "now").mockReturnValue(Date.now() + 1000);
  const file = new File(["native file"], "note.txt", { type: "text/plain" });
  const paths = vi.spyOn(host(), "pathsForFiles").mockReturnValue(["/workspace/note.txt"]);
  const dataTransfer = { files: [file], types: ["Files"], getData: () => "" };
  fireEvent.drop(box, { dataTransfer });
  await screen.findByRole("button", { name: "移除 note.txt" });
  drag(row, box);
  await screen.findByRole("button", { name: "移除 README.md" });
  expect(paths).toHaveBeenCalledExactlyOnceWith([file]);
  expect(dropRefs.mock.calls).toEqual([[["/workspace/note.txt"]], [["/workspace/README.md"]]]);
  expect(attach).not.toHaveBeenCalled();
});
