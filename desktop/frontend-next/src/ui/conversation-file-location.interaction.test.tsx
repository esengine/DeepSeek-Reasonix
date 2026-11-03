// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { useState } from "react";
import "./testkit";
import { Pane } from "./Pane";
import { MockPort } from "../port/mock";
import type { RuntimeView } from "../port/hub";
import { boot, STORAGE } from "../i18n";

const path = "docs/nested/report.md";
afterEach(() => { cleanup(); vi.restoreAllMocks(); localStorage.clear(); sessionStorage.clear(); });

async function mount(delivery = false, remote = false) {
  localStorage.setItem(STORAGE, "zh"); boot();
  vi.spyOn(HTMLCanvasElement.prototype, "getContext").mockReturnValue(null);
  const port = new MockPort();
  const status = await port.status();
  vi.spyOn(port, "status").mockResolvedValue({ ...status, workspaceRoot: "/work" });
  vi.spyOn(port, "history").mockResolvedValue(delivery ? [
    { role: "user", content: "Create a report" },
    { role: "assistant", content: "", toolCalls: [{ id: "delivery", name: "present_files" }] },
    { role: "tool", content: "done", toolCallId: "delivery", toolName: "present_files", deliveries: [{ path }] },
    { role: "assistant", content: "Report ready." },
  ] : [{ role: "user", content: "Create a report" }, { role: "assistant", content: `[Report](/work/${path})` }]);
  vi.spyOn(port, "workspaceFiles").mockImplementation(async (folder) => ({
    files: folder === "docs/nested" ? [path] : [],
    directories: folder === "" ? ["docs"] : folder === "docs" ? ["docs/nested"] : [],
  }));
  vi.spyOn(port, "workspaceFile").mockResolvedValue({ path, content: "# Report", revision: "1" });
  vi.spyOn(port, "revealsFiles").mockReturnValue(true);
  function Harness() {
    const [manualBrowser, onManualBrowser] = useState(false);
    return <Pane port={port} rt={{ id: "p1", root: "/work", name: "work", host: remote ? "server" : undefined } as RuntimeView}
      title="work" active visible sideHost={null} side={false} onFocus={() => {}} onReport={() => {}} onSessionChanged={() => {}}
      pulse={0} findPulse={0} onSettings={() => {}} needsProject={false} onOpenProject={() => {}} onKeepHere={() => {}}
      theme="dark" dockW={560} dockMax={880} onDockW={() => {}} manualBrowser={manualBrowser} onManualBrowser={onManualBrowser} />;
  }
  await act(async () => { render(<Harness />); });
  return port;
}

async function expectLocated() {
  const tree = await screen.findByRole("region", { name: "文件" });
  expect(within(tree).getByRole("heading", { name: "文件" })).toBeTruthy();
  await waitFor(() => {
    const row = document.querySelector('.workbench-tree-row[aria-current="page"]');
    expect(row?.getAttribute("data-target")).toBe(path);
    expect(document.querySelector(".workbench-body")?.hasAttribute("data-files")).toBe(true);
  });
  for (const folder of ["docs", "docs/nested"]) {
    expect(document.querySelector(`[data-target="${folder}"][aria-expanded]`)?.getAttribute("aria-expanded")).toBe("true");
  }
}

it("locates a conversation link in its expanded file tree and retains preview navigation", async () => {
  await mount();
  fireEvent.contextMenu(await screen.findByText("Report"));
  fireEvent.click(await screen.findByRole("menuitem", { name: "在文件树中显示" }));
  await expectLocated();
  fireEvent.click(document.querySelector('.workbench-tree-row[aria-current="page"]')!);
  await screen.findByRole("heading", { name: "Report" });
  expect(document.querySelector(".workbench-body")?.hasAttribute("data-files")).toBe(false);
});

it("offers tree location from a remote delivered file without local shell actions", async () => {
  await mount(true, true);
  fireEvent.contextMenu(await screen.findByRole("button", { name: "预览 report.md" }));
  const locate = await screen.findByRole("menuitem", { name: "在文件树中显示" });
  expect(screen.queryByRole("menuitem", { name: "在文件夹中显示" })).toBeNull();
  fireEvent.click(locate);
  await expectLocated();
});

it("opens a delivered file's actions using the keyboard context-menu key", async () => {
  await mount(true);
  const preview = await screen.findByRole("button", { name: "预览 report.md" });
  preview.focus();
  fireEvent.keyDown(preview, { key: "F10", shiftKey: true });
  const locate = await screen.findByRole("menuitem", { name: "在文件树中显示" });
  expect(screen.getByRole("menuitem", { name: "在文件夹中显示" })).toBeTruthy();
  fireEvent.click(locate);
  await expectLocated();
});

it("reveals the same delivery again after collapsing its parents and filtering the tree", async () => {
  await mount(true);
  const locate = async () => {
    fireEvent.contextMenu(await screen.findByRole("button", { name: "预览 report.md" }));
    fireEvent.click(await screen.findByRole("menuitem", { name: "在文件树中显示" }));
  };
  await locate();
  await expectLocated();
  fireEvent.click(screen.getByRole("region", { name: "文件" }).querySelector('[data-target="docs"][aria-expanded]')!);
  expect(document.querySelector('.workbench-tree-row[aria-current="page"]')).toBeNull();
  await locate();
  await expectLocated();
  fireEvent.change(screen.getByRole("textbox", { name: "搜索文件" }), { target: { value: "missing" } });
  await locate();
  await expectLocated();
  expect(screen.getByRole("textbox", { name: "搜索文件" })).toHaveProperty("value", "");
});
