// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { MockPort } from "../port/mock";
import { WorkbenchPanel } from "./WorkbenchPanel";
import { host } from "../port/host";
import { HttpError } from "../port/http_error";
import "./testkit";

afterEach(() => { cleanup(); vi.restoreAllMocks(); vi.unstubAllGlobals(); });

const gone = () => new HttpError(404, "missing", { code: "workspace.file_missing", error: "no such file" });
const props = (port: MockPort) => ({ port, tabs: [], manual: false, shown: true, scheme: "light" as const, changes: [], workspaceRoot: "/one", onCloseManual: vi.fn(), onSurfaces: vi.fn(), onExternal: vi.fn() });

it("says a requested file cannot be found instead of leaving the panel blank", async () => {
  const port = new MockPort();
  vi.spyOn(port, "workspaceFiles").mockResolvedValue({ files: ["real.md"], directories: [] });
  vi.spyOn(port, "workspaceFile").mockRejectedValue(gone());
  const view = render(<WorkbenchPanel {...props(port)} />);
  view.rerender(<WorkbenchPanel {...props(port)} fileRequest={{ path: "docs/missing.md" }} />);
  const alert = await screen.findByRole("alert");
  expect(alert.textContent).toContain("文件已不在原处");
  expect(alert.textContent).toContain("可能被删除或移动了");
  await waitFor(() => expect(document.querySelector(".workbench-read, .cm-content")).toBeNull());
});

it("reads a vanished file again on request, and closes its tab from the same card", async () => {
  const port = new MockPort();
  vi.spyOn(port, "workspaceFiles").mockResolvedValue({ files: ["real.md"], directories: [] });
  const read = vi.spyOn(port, "workspaceFile").mockRejectedValue(gone());
  const view = render(<WorkbenchPanel {...props(port)} />);
  view.rerender(<WorkbenchPanel {...props(port)} fileRequest={{ path: "docs/missing.md" }} />);
  await screen.findByRole("button", { name: "重新读取" });
  const before = read.mock.calls.length;
  fireEvent.click(screen.getByRole("button", { name: "重新读取" }));
  await waitFor(() => expect(read.mock.calls.length).toBeGreaterThan(before));
  fireEvent.click(screen.getByRole("button", { name: "关闭页签" }));
  await waitFor(() => expect(screen.queryByRole("tab", { name: "missing.md" })).toBeNull());
});

it("does not open a preview page for an HTML file that cannot be read", async () => {
  const port = new MockPort();
  vi.spyOn(port, "workspaceFiles").mockResolvedValue({ files: [], directories: [] });
  vi.spyOn(port, "workspaceFile").mockRejectedValue(gone());
  vi.spyOn(port, "workspaces").mockResolvedValue({ current: "/one", canSwitch: true, canIsolate: true, recents: [] });
  vi.spyOn(host(), "drawsBrowserViews").mockReturnValue(true);
  vi.spyOn(host(), "controlBrowserView").mockImplementation(() => {});
  vi.stubGlobal("ResizeObserver", class { observe() {} disconnect() {} unobserve() {} });
  const open = vi.spyOn(port, "browserOpen").mockResolvedValue({ id: "p", target: "t", url: "file:///one/missing.html", title: "", active: false });
  const view = render(<WorkbenchPanel {...props(port)} />);
  view.rerender(<WorkbenchPanel {...props(port)} fileRequest={{ path: "missing.html" }} />);
  const alert = await screen.findByRole("alert");
  expect(alert.textContent).toContain("找不到该文件");
  await new Promise((r) => setTimeout(r, 50));
  expect(open).not.toHaveBeenCalled();
});

it("allows explicit interactive opening when static HTML exceeds its limit", async () => {
  const port = new MockPort();
  vi.spyOn(port, "workspaceFiles").mockResolvedValue({ files: ["big.html"], directories: [] });
  vi.spyOn(port, "workspaceFile").mockRejectedValue(new HttpError(400, "unreadable", { code: "workspace.file_unreadable" }));
  vi.spyOn(port, "workspaces").mockResolvedValue({ current: "/one", canSwitch: true, canIsolate: true, recents: [] });
  vi.spyOn(host(), "drawsBrowserViews").mockReturnValue(true);
  vi.spyOn(host(), "controlBrowserView").mockImplementation(() => {});
  vi.stubGlobal("ResizeObserver", class { observe() {} disconnect() {} unobserve() {} });
  const open = vi.spyOn(port, "browserOpen").mockResolvedValue({ id: "p", target: "t", url: "file:///one/big.html", title: "", active: false });
  const view = render(<WorkbenchPanel {...props(port)} />);
  view.rerender(<WorkbenchPanel {...props(port)} fileRequest={{ path: "big.html" }} />);
  fireEvent.click(await screen.findByRole("button", { name: "交互运行" }));
  await waitFor(() => expect(open).toHaveBeenCalled());
});

it("shows metadata and a typed error for an unknown binary instead of an editor", async () => {
  const port = new MockPort();
  vi.spyOn(port, "workspaceFiles").mockResolvedValue({ files: ["opaque.data"], directories: [] });
  vi.spyOn(port, "workspaceFile").mockRejectedValue(new HttpError(400, "unreadable", { code: "workspace.file_unreadable" }));
  vi.spyOn(port, "workspacePreviewInfo").mockResolvedValue({
    ref: { path: "opaque.data" }, kind: "text", size: 128, limit: 2 << 20, version: "v1", readOnly: false,
  });
  vi.spyOn(port, "workspacePreviewContent").mockRejectedValue(new HttpError(415, "binary", { code: "workspace.preview_not_text" }));
  const view = render(<WorkbenchPanel {...props(port)} />);
  view.rerender(<WorkbenchPanel {...props(port)} fileRequest={{ path: "opaque.data" }} />);
  await waitFor(() => expect(document.querySelector(".document-preview [role=alert]")?.textContent).toContain("文本"));
  expect(screen.getByText("128 B")).toBeTruthy();
  expect(document.querySelector(".cm-content")).toBeNull();
});
