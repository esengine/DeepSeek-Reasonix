// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { useState } from "react";
import "./testkit";
import { Pane } from "./Pane";
import { MockPort } from "../port/mock";
import type { RuntimeView } from "../port/hub";
import { HttpError } from "../port/http_error";
import { host } from "../port/host";
import { boot, STORAGE } from "../i18n";

afterEach(() => { cleanup(); vi.restoreAllMocks(); vi.unstubAllGlobals(); localStorage.clear(); sessionStorage.clear(); });

async function mount(remote = false, existingBrowser = false) {
  localStorage.setItem(STORAGE, "zh"); boot();
  vi.spyOn(HTMLCanvasElement.prototype, "getContext").mockReturnValue(null);
  const port = new MockPort();
  if (existingBrowser) vi.spyOn(port, "browserTabs").mockResolvedValue([{ id: "b1", target: "t1", url: "https://example.com", title: "Agent page", active: true }]);
  const status = await port.status();
  vi.spyOn(port, "status").mockResolvedValue({ ...status, workspaceRoot: "/work" });
  vi.spyOn(port, "history").mockResolvedValue([{ role: "user", content: "Create a report" }, { role: "assistant", content: "[Report](/work/docs/report.md) and `/work/docs/missing.md` [Image](images/chart.png)" }]);
  const read = vi.spyOn(port, "workspaceFile").mockImplementation(async (path) => {
    if (path === "docs/missing.md") throw new HttpError(404, "missing", { code: "workspace.file_missing" }, true);
    return { path, content: "# Generated report", revision: "1" };
  });
  const reveal = vi.spyOn(port, "revealInFileManager").mockResolvedValue();
  const openFile = vi.spyOn(port, "openFile").mockResolvedValue();
  vi.spyOn(port, "opensFiles").mockReturnValue(true);
  vi.spyOn(port, "revealsFiles").mockReturnValue(true);
  vi.spyOn(host(), "describe").mockResolvedValue({ ...await host().describe(), platform: "darwin" });
  const browser = vi.spyOn(port, "browserOpen");
  function Harness() {
    const [manualBrowser, onManualBrowser] = useState(false);
    return <Pane port={port} rt={{ id: "p1", root: "/work", name: "work", host: remote ? "server" : undefined } as RuntimeView}
      title="work" active visible sideHost={null} side={false} onFocus={() => {}} onReport={() => {}} onSessionChanged={() => {}}
      pulse={0} findPulse={0} onSettings={() => {}} needsProject={false} onOpenProject={() => {}} onKeepHere={() => {}}
      theme="dark" dockW={560} dockMax={880} onDockW={() => {}} manualBrowser={manualBrowser} onManualBrowser={onManualBrowser} />;
  }
  await act(async () => { render(<Harness />); });
  await screen.findByText("Report");
  return { port, read, reveal, browser, openFile };
}

it("opens a restored reply's file in the right column and reuses its tab on repeated clicks", async () => {
  const { read, browser } = await mount();
  fireEvent.click(screen.getByText("Report"));
  await screen.findByRole("heading", { name: "Generated report" });
  expect(read).toHaveBeenCalledWith("docs/report.md");
  expect(document.querySelector('[data-pane="browser"]')!.hasAttribute("inert")).toBe(false);
  fireEvent.click(screen.getByText("Report"));
  expect(screen.getAllByRole("tab", { name: "report.md" })).toHaveLength(1);
  expect(browser).not.toHaveBeenCalled();
  fireEvent.click(screen.getByText("/work/docs/missing.md"));
  expect((await screen.findByRole("alert")).textContent).toContain("文件已不在原处");
});

it("offers file actions on right-click and restores focus to the link", async () => {
  const { reveal } = await mount();
  fireEvent.contextMenu(screen.getByText("Report"));
  fireEvent.click(await screen.findByRole("menuitem", { name: "在访达中显示" }));
  await waitFor(() => expect(reveal).toHaveBeenCalledExactlyOnceWith("docs/report.md"));
  expect(screen.queryByRole("menu")).toBeNull();
  expect(document.activeElement).toBe(screen.getByText("Report"));
  fireEvent.keyDown(screen.getByText("Report"), { key: "F10", shiftKey: true });
  expect(screen.getByRole("menuitem", { name: "复制路径" })).toBeTruthy();
  fireEvent.keyDown(window, { key: "Escape" });
  expect(screen.queryByRole("menu")).toBeNull();
});

it("keeps remote paths out of the local file manager", async () => {
  const { reveal } = await mount(true);
  fireEvent.contextMenu(screen.getByText("Report"));
  expect(await screen.findByRole("menuitem", { name: "复制路径" })).toBeTruthy();
  expect(screen.queryByRole("menuitem", { name: "在访达中显示" })).toBeNull();
  expect(reveal).not.toHaveBeenCalled();
  expect(screen.queryByRole("menuitem", { name: "使用系统默认应用打开" })).toBeNull();
});

it("opens the selected file in its default application only on request", async () => {
  const { openFile } = await mount();
  expect(openFile).not.toHaveBeenCalled();
  fireEvent.contextMenu(screen.getByText("Report"));
  fireEvent.click(await screen.findByRole("menuitem", { name: "使用系统默认应用打开" }));
  expect(openFile).toHaveBeenCalledExactlyOnceWith("docs/report.md");
});

it("previews images without attempting to edit their binary content", async () => {
  const { read } = await mount();
  fireEvent.click(screen.getByText("Image"));
  const image = await screen.findByAltText("images/chart.png");
  expect(image.getAttribute("src")).toMatch(/^blob:/);
  expect(read).not.toHaveBeenCalledWith("images/chart.png");
  fireEvent.load(image);
  expect(image.hasAttribute("hidden")).toBe(false);
  fireEvent.error(image);
  expect(screen.getByRole("alert").textContent).toContain("图片不可用");
});

it("keeps a conversation file selected when the column already has an agent browser", async () => {
  await mount(false, true);
  fireEvent.click(screen.getByText("Report"));
  await screen.findByRole("heading", { name: "Generated report" });
  expect(screen.getByRole("tab", { name: "report.md" }).getAttribute("aria-selected")).toBe("true");
});
