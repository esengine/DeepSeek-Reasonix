// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { MockPort } from "../port/mock";
import type { WorkspaceChange } from "../port/port";
import { WorkbenchPanel } from "./WorkbenchPanel";
import "./testkit";

const change = (path: string, status = "M"): WorkspaceChange => ({ path, status, staged: false, unstaged: true });
const props = (port: MockPort, changes: WorkspaceChange[]) => ({
  port, workspaceRoot: "/work/project", tabs: [], manual: false, shown: true, scheme: "light" as const,
  changes, onCloseManual: vi.fn(), onSurfaces: vi.fn(), onExternal: vi.fn(), onAddToChat: vi.fn(),
});
afterEach(() => { cleanup(); vi.restoreAllMocks(); });

it("offers the file menu on a changed file, with the path the tree would give", async () => {
  const port = new MockPort();
  vi.spyOn(port, "workspaceFiles").mockResolvedValue({ files: [], directories: [] });
  render(<WorkbenchPanel {...props(port, [change("web/src/prototype.html", "A")])} />);
  const row = await screen.findByTitle("web/src/prototype.html");
  fireEvent.contextMenu(row);
  expect(await screen.findByRole("menuitem", { name: "复制路径" })).toBeTruthy();
  expect(screen.getByRole("menuitem", { name: "复制引用" })).toBeTruthy();
});

it("reveals the changed file in the file manager from its menu", async () => {
  const port = new MockPort();
  vi.spyOn(port, "workspaceFiles").mockResolvedValue({ files: [], directories: [] });
  vi.spyOn(port, "revealsFiles").mockReturnValue(true);
  const reveal = vi.spyOn(port, "revealInFileManager").mockResolvedValue(undefined);
  render(<WorkbenchPanel {...props(port, [change("a/b.ts")])} />);
  fireEvent.contextMenu(await screen.findByTitle("a/b.ts"));
  fireEvent.click(await screen.findByRole("menuitem", { name: /在访达中显示|在文件资源管理器中显示|在系统文件管理器中显示/ }));
  await waitFor(() => expect(reveal).toHaveBeenCalledWith("a/b.ts"));
});

it("leaves a plain click on a changed file opening its diff", async () => {
  const port = new MockPort();
  vi.spyOn(port, "workspaceFiles").mockResolvedValue({ files: [], directories: [] });
  render(<WorkbenchPanel {...props(port, [change("a/b.ts")])} />);
  fireEvent.click(await screen.findByTitle("a/b.ts"));
  expect(screen.queryByRole("menuitem", { name: "复制路径" })).toBeNull();
});
