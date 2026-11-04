// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, it, vi } from "vitest";
import { MockPort } from "../port/mock";
import { WorkbenchPanel } from "./WorkbenchPanel";
import { boot, STORAGE } from "../i18n";

const props = (port = new MockPort(), workspaceRoot = "/work/project") => ({
  port, workspaceRoot, tabs: [], manual: false, shown: true, scheme: "light" as const,
  changes: [], onCloseManual: vi.fn(), onSurfaces: vi.fn(), onExternal: vi.fn(),
});
afterEach(() => { cleanup(); vi.restoreAllMocks(); });

it.each([
  ["/work/project", "src/你好 [draft].ts", false, "/work/project/src/你好 [draft].ts", "@src/你好\\ [draft].ts"],
  ["C:\\My Project", "src/two words.ts", false, "C:\\My Project\\src\\two words.ts", "@src/two\\ words.ts"],
  ["\\\\server\\share\\", "docs/team notes", true, "\\\\server\\share\\docs\\team notes", "@docs/team\\ notes/"],
  ["/", "a\tb", true, "/a\tb", "@a\\\tb/"],
  ["/work", 'name"', false, '/work/name"', '@name"'],
  ["/work", 'quoted-dir"', true, '/work/quoted-dir"', '@quoted-dir"/'],
])("copies paths and references from %s without opening or submitting", async (root, path, folder, absolute, reference) => {
  const user = userEvent.setup();
  const write = vi.spyOn(navigator.clipboard, "writeText").mockResolvedValue();
  const port = new MockPort();
  vi.spyOn(port, "workspaceFiles").mockResolvedValue({ files: folder ? [] : [path], directories: folder ? [path] : [] });
  const read = vi.spyOn(port, "workspaceFile"), submit = vi.spyOn(port, "submit"), drop = vi.spyOn(port, "dropRefs");
  const mcp = vi.spyOn(port, "mcp");
  render(<WorkbenchPanel {...props(port, root)} />);
  const row = await screen.findByRole("button", { name: (name) => name.trim().replace(/\s+/g, " ") === path.split("/").at(-1)?.trim().replace(/\s+/g, " ") });
  for (const [label, text] of [["复制路径", absolute], ["复制引用", reference]]) {
    fireEvent.contextMenu(row);
    await user.click(screen.getByRole("menuitem", { name: label }));
    await waitFor(() => expect(write).toHaveBeenLastCalledWith(text));
    expect(screen.queryByRole("menu")).toBeNull();
  }
  expect(read).not.toHaveBeenCalled();
  expect(submit).not.toHaveBeenCalled();
  expect(drop).not.toHaveBeenCalled();
  expect(mcp).not.toHaveBeenCalled();
});

it.each([false, true])("allows copies without a local reveal (remote: %s)", async (remote) => {
  const user = userEvent.setup();
  const write = vi.spyOn(navigator.clipboard, "writeText").mockResolvedValue();
  const port = new MockPort();
  vi.spyOn(port, "revealsFiles").mockReturnValue(false);
  render(<WorkbenchPanel {...props(port, "/remote/project")} remote={remote} />);
  fireEvent.contextMenu(await screen.findByRole("button", { name: "README.md" }));
  expect(screen.queryByRole("menuitem", { name: /文件管理器/ })).toBeNull();
  await user.click(screen.getByRole("menuitem", { name: "复制路径" }));
  expect(write).toHaveBeenCalledWith("/remote/project/README.md");
});

it.each(["", "~/project", "relative/path", "C:project"])("does not invent an absolute root for %s", async (root) => {
  render(<WorkbenchPanel {...props(undefined, root)} />);
  fireEvent.contextMenu(await screen.findByRole("button", { name: "README.md" }));
  expect((screen.getByRole("menuitem", { name: "复制路径" }) as HTMLButtonElement).disabled).toBe(true);
  expect((screen.getByRole("menuitem", { name: "复制引用" }) as HTMLButtonElement).disabled).toBe(false);
});

it("keeps a failed clipboard write retryable and restores focus after choosing", async () => {
  const user = userEvent.setup();
  const write = vi.spyOn(navigator.clipboard, "writeText").mockRejectedValueOnce(new Error("denied")).mockResolvedValue();
  render(<WorkbenchPanel {...props()} />);
  const row = await screen.findByRole("button", { name: "README.md" });
  fireEvent.contextMenu(row);
  await user.click(screen.getByRole("menuitem", { name: "复制引用" }));
  expect((await screen.findByRole("alert")).textContent).toBe("复制失败，请重试");
  await user.click(screen.getByRole("menuitem", { name: "复制引用" }));
  await waitFor(() => expect(screen.queryByRole("menu")).toBeNull());
  expect(write).toHaveBeenCalledTimes(2);
  expect(document.activeElement).toBe(row);
});

it("navigates the menu by arrows, activates with Space, and cancels with Escape", async () => {
  const user = userEvent.setup();
  const write = vi.spyOn(navigator.clipboard, "writeText").mockResolvedValue();
  render(<WorkbenchPanel {...props()} />);
  const row = await screen.findByRole("button", { name: "README.md" });
  fireEvent.contextMenu(row);
  const target = screen.getByRole("menuitem", { name: "复制引用" });
  const items = screen.getAllByRole("menuitem").filter((x) => !(x as HTMLButtonElement).disabled);
  for (let i = 0; i < items.length && document.activeElement !== target; i++) await user.keyboard("{ArrowDown}");
  expect(document.activeElement).toBe(target);
  await user.keyboard(" ");
  expect(write).toHaveBeenCalledWith("@README.md");
  fireEvent.contextMenu(row);
  await user.keyboard("{Escape}");
  expect(screen.queryByRole("menu")).toBeNull();
  expect(document.activeElement).toBe(row);
});

it.each([false, true])("ignores an old clipboard completion after workspace replacement (failure: %s)", async (failure) => {
  const user = userEvent.setup();
  let finish!: () => void;
  vi.spyOn(navigator.clipboard, "writeText").mockImplementationOnce(() => new Promise<void>((resolve, reject) => { finish = failure ? () => reject(new Error("denied")) : resolve; }));
  const first = props();
  const { rerender } = render(<WorkbenchPanel {...first} />);
  fireEvent.contextMenu(await screen.findByRole("button", { name: "README.md" }));
  await user.click(screen.getByRole("menuitem", { name: "复制引用" }));
  const second = props(new MockPort(), "/other/project");
  rerender(<WorkbenchPanel {...second} />);
  expect(screen.queryByRole("menu")).toBeNull();
  await waitFor(() => expect(screen.getByRole("button", { name: "README.md" })).toBeTruthy());
  fireEvent.contextMenu(screen.getByRole("button", { name: "README.md" }));
  await act(async () => finish());
  expect(screen.getByRole("menu")).toBeTruthy();
  expect(screen.queryByRole("alert")).toBeNull();
});

it("does not copy an old row against a new root while its tree is loading", async () => {
  const port = new MockPort();
  const first = props(port);
  const { rerender } = render(<WorkbenchPanel {...first} />);
  const row = await screen.findByRole("button", { name: "README.md" });
  fireEvent.contextMenu(row);
  let finish!: (value: { files: string[]; directories: string[] }) => void;
  vi.spyOn(port, "workspaceFiles").mockImplementation(() => new Promise((resolve) => { finish = resolve; }));
  rerender(<WorkbenchPanel {...first} workspaceRoot="/second" />);
  expect(screen.queryByRole("menu")).toBeNull();
  expect(fireEvent.contextMenu(row)).toBe(true);
  await act(async () => finish({ files: ["new.txt"], directories: [] }));
  fireEvent.contextMenu(screen.getByRole("button", { name: "new.txt" }));
  expect(screen.getByRole("menuitem", { name: "复制路径" })).toBeTruthy();
});

it("dismisses on outside click, Tab, and hiding without resurrecting the menu", async () => {
  const user = userEvent.setup();
  const first = props();
  const { rerender } = render(<WorkbenchPanel {...first} />);
  const row = await screen.findByRole("button", { name: "README.md" });
  fireEvent.contextMenu(row);
  await user.click(screen.getByRole("textbox", { name: "搜索文件" }));
  expect(screen.queryByRole("menu")).toBeNull();
  fireEvent.contextMenu(row);
  await user.tab();
  expect(screen.queryByRole("menu")).toBeNull();
  fireEvent.contextMenu(row);
  rerender(<WorkbenchPanel {...first} shown={false} />);
  rerender(<WorkbenchPanel {...first} />);
  expect(screen.queryByRole("menu")).toBeNull();
});

it.each(["line\nbreak.txt", "notes)"])("does not copy an unparseable reference for %j", async (path) => {
  const port = new MockPort();
  vi.spyOn(port, "workspaceFiles").mockResolvedValue({ files: [path], directories: [] });
  render(<WorkbenchPanel {...props(port)} />);
  await waitFor(() => expect(document.querySelector(".workbench-tree-row")).toBeTruthy());
  fireEvent.contextMenu(document.querySelector(".workbench-tree-row")!);
  expect((screen.getByRole("menuitem", { name: "复制引用" }) as HTMLButtonElement).disabled).toBe(true);
  expect((screen.getByRole("menuitem", { name: "复制路径" }) as HTMLButtonElement).disabled).toBe(false);
});

it("leaves selected text to the browser's context menu", async () => {
  render(<WorkbenchPanel {...props()} />);
  const row = await screen.findByRole("button", { name: "README.md" });
  vi.spyOn(document, "getSelection").mockReturnValue({ isCollapsed: false } as Selection);
  expect(fireEvent.contextMenu(row)).toBe(true);
  expect(screen.queryByRole("menu")).toBeNull();
});

it("labels copy actions in English", async () => {
  localStorage.setItem(STORAGE, "en");
  boot();
  try {
    render(<WorkbenchPanel {...props()} />);
    fireEvent.contextMenu(await screen.findByRole("button", { name: "README.md" }));
    expect(screen.getByRole("menuitem", { name: "Copy path" })).toBeTruthy();
    expect(screen.getByRole("menuitem", { name: "Copy reference" })).toBeTruthy();
  } finally {
    localStorage.setItem(STORAGE, "zh");
    boot();
    localStorage.removeItem(STORAGE);
  }
});


it.each([
  ["github:notes.md", false, false],
  ["github:team notes.md", false, true],
  ["github:dir", true, true],
  ["src/github:notes.md", false, true],
  ["src/github:dir", true, true],
])("keeps path copy but disables ambiguous MCP references for %s", async (path, folder, remote) => {
  const user = userEvent.setup();
  const write = vi.spyOn(navigator.clipboard, "writeText").mockResolvedValue();
  const port = new MockPort();
  vi.spyOn(port, "workspaceFiles").mockResolvedValue({ files: folder ? [] : [path], directories: folder ? [path] : [] });
  const read = vi.spyOn(port, "workspaceFile"), submit = vi.spyOn(port, "submit"), drop = vi.spyOn(port, "dropRefs"), mcp = vi.spyOn(port, "mcp");
  render(<WorkbenchPanel {...props(port, "/work")} remote={remote} />);
  fireEvent.contextMenu(await screen.findByRole("button", { name: path.split("/").at(-1) }));
  const reference = screen.getByRole("menuitem", { name: "复制引用" }) as HTMLButtonElement;
  expect(reference.disabled).toBe(true);
  expect(reference.title).toBe("此名称可能被识别为 MCP 资源");
  await user.click(reference);
  expect(write).not.toHaveBeenCalled();
  await user.click(screen.getByRole("menuitem", { name: "复制路径" }));
  expect(write).toHaveBeenCalledExactlyOnceWith(`/work/${path}`);
  for (const action of [read, submit, drop, mcp]) expect(action).not.toHaveBeenCalled();
});
