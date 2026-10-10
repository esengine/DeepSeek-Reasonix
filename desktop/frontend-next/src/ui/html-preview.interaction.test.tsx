// @vitest-environment jsdom
import { act, cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { MockPort } from "../port/mock";
import * as hostModule from "../port/host";
import type { BrowserTab } from "../port/port";
import { SsePort } from "../port/sse";
import { WorkbenchPanel } from "./WorkbenchPanel";

const ROOT = "/ws/site";
const page = (over: Partial<BrowserTab> = {}): BrowserTab => ({
  id: "p1",
  target: "t-preview",
  url: `file://${ROOT}/index.html`,
  title: "Landing",
  active: false,
  ...over,
});

function setup(options: { views?: boolean; revision?: () => string } = {}) {
  const { views = true, revision = () => "r1" } = options;
  const shell = hostModule.host();
  vi.spyOn(shell, "drawsBrowserViews").mockReturnValue(views);
  const control = vi.spyOn(shell, "controlBrowserView").mockImplementation(() => {});
  const port = new MockPort();
  vi.spyOn(port, "workspaces").mockResolvedValue({ current: ROOT, canSwitch: true, canIsolate: true, recents: [] });
  vi.spyOn(port, "workspaceFiles").mockResolvedValue({ files: ["index.html", "about.htm", "README.md", "notes.txt"], directories: [] });
  vi.spyOn(port, "workspaceFile").mockImplementation(async (path) => ({ path, content: `<h1>${path}</h1>`, revision: revision() }));
  const open = vi.spyOn(port, "browserOpen").mockResolvedValue(page());
  return { port, open, control };
}

type Props = Partial<React.ComponentProps<typeof WorkbenchPanel>>;
const panel = (port: MockPort, extra: Props = {}) => (
  <WorkbenchPanel port={port} tabs={[]} manual={false} shown scheme="light" changes={[]} onCloseManual={vi.fn()} onSurfaces={vi.fn()} onExternal={vi.fn()} {...extra} />
);

beforeEach(() => {
  vi.restoreAllMocks();
  vi.stubGlobal("ResizeObserver", class { observe() {} disconnect() {} unobserve() {} });
});
afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

describe("opening an HTML file from the explorer", () => {
  it("shows the file as a page on click, through the browser's own file path", async () => {
    const user = userEvent.setup();
    const { port, open } = setup();
    const { rerender } = render(panel(port));
    await user.click(await screen.findByRole("button", { name: "index.html" }));
    await waitFor(() => expect(open).toHaveBeenCalledTimes(1));
    expect(open).toHaveBeenCalledWith(`${ROOT}/index.html`, true);
    rerender(panel(port, { tabs: [page()] }));
    await waitFor(() => expect(document.querySelector(".workbench-canvas .bpanel")).toBeTruthy());
    expect(screen.getByRole("button", { name: "预览" }).getAttribute("aria-pressed")).toBe("true");
    expect(document.querySelector(".cm-content")).toBeNull();
  });

  it("does not turn the page it opened for the file into a second tab", async () => {
    const user = userEvent.setup();
    const { port, open } = setup();
    // The kernel reports the tab before the open call answers.
    let answer!: (tab: BrowserTab) => void;
    open.mockReturnValue(new Promise((resolve) => (answer = resolve)));
    const { rerender } = render(panel(port));
    await user.click(await screen.findByRole("button", { name: "index.html" }));
    await waitFor(() => expect(open).toHaveBeenCalled());
    rerender(panel(port, { tabs: [page()] }));
    expect(screen.queryByRole("tab", { name: "Landing" })).toBeNull();
    await act(async () => answer(page()));
    expect(screen.queryByRole("tab", { name: "Landing" })).toBeNull();
    expect(screen.getAllByRole("tab").map((tab) => tab.textContent)).toEqual(["index.html"]);
    expect(screen.getByRole("tab", { name: "index.html" }).getAttribute("aria-selected")).toBe("true");
  });

  it("opens from the keyboard with Enter on the focused row", async () => {
    const user = userEvent.setup();
    const { port, open } = setup();
    render(panel(port));
    (await screen.findByRole("button", { name: "about.htm" })).focus();
    await user.keyboard("{Enter}");
    await waitFor(() => expect(open).toHaveBeenCalledWith(`${ROOT}/about.htm`, true));
  });

  it("opens the file once however often it is picked, and keeps the page across views", async () => {
    const user = userEvent.setup();
    const { port, open } = setup();
    const { rerender } = render(panel(port));
    await user.click(await screen.findByRole("button", { name: "index.html" }));
    await waitFor(() => expect(open).toHaveBeenCalledTimes(1));
    rerender(panel(port, { tabs: [page()] }));
    await user.click(await screen.findByRole("button", { name: "编辑" }));
    await waitFor(() => expect(document.querySelector(".cm-content")).toBeTruthy());
    await user.click(screen.getByRole("button", { name: "预览" }));
    await user.click(screen.getAllByRole("button", { name: "index.html" }).find((b) => b.classList.contains("workbench-tree-row"))!);
    expect(open).toHaveBeenCalledTimes(1);
    expect(document.querySelector(".workbench-canvas .bpanel")).toBeTruthy();
  });

  it("reloads the page when the file changes on disk, and not before", async () => {
    const user = userEvent.setup();
    let revision = "r1";
    const { port, open, control } = setup({ revision: () => revision });
    const { rerender } = render(panel(port));
    await user.click(await screen.findByRole("button", { name: "index.html" }));
    await waitFor(() => expect(open).toHaveBeenCalled());
    rerender(panel(port, { tabs: [page()], wrote: 0 }));
    await waitFor(() => expect(document.querySelector(".bpanel")).toBeTruthy());
    rerender(panel(port, { tabs: [page()], wrote: 1 }));
    await new Promise((r) => setTimeout(r, 30));
    expect(control).not.toHaveBeenCalled();
    revision = "r2";
    rerender(panel(port, { tabs: [page()], wrote: 2 }));
    await waitFor(() => expect(control).toHaveBeenCalledWith("t-preview", "reload"));
    expect(control).toHaveBeenCalledTimes(1);
  });

  it("finds the page already open after the window reloads rather than opening another", async () => {
    const user = userEvent.setup();
    const { port, open } = setup();
    render(panel(port, { tabs: [page()] }));
    expect(screen.getByRole("tab", { name: "Landing" })).toBeTruthy();
    await user.click(await screen.findByRole("button", { name: "index.html" }));
    await waitFor(() => expect(screen.queryByRole("tab", { name: "Landing" })).toBeNull());
    expect(open).not.toHaveBeenCalled();
    expect(document.querySelector(".workbench-canvas .bpanel")).toBeTruthy();
  });

  it("reads a Windows workspace the way the browser does", async () => {
    const user = userEvent.setup();
    const { port, open } = setup();
    vi.spyOn(port, "workspaces").mockResolvedValue({ current: "D:\\proj\\site\\", canSwitch: true, canIsolate: true, recents: [] });
    const { rerender } = render(panel(port));
    await user.click(await screen.findByRole("button", { name: "index.html" }));
    await waitFor(() => expect(open).toHaveBeenCalledWith("D:\\proj\\site/index.html", true));
    open.mockClear();
    rerender(panel(port, { tabs: [page({ url: "file:///D:/proj/site/index.html" })] }));
    await waitFor(() => expect(document.querySelector(".bpanel")).toBeTruthy());
    expect(open).not.toHaveBeenCalled();
  });

  describe("when the browser refuses", () => {
    const refusedLikeTheKernel = (port: MockPort, code: string, detail: string) => {
      vi.stubGlobal("fetch", async () => new Response(JSON.stringify({ code, error: detail, params: { url: "x", error: detail } }), { status: 400 }));
      const wire = new SsePort("", "r1");
      vi.spyOn(port, "browserOpen").mockImplementation((url: string) => wire.browserOpen(url, true));
    };

    it("says why and leaves the file's text to read", async () => {
      const user = userEvent.setup();
      const { port } = setup();
      refusedLikeTheKernel(port, "browser.open_failed", "browser.url_refused: outside the workspace");
      render(panel(port));
      await user.click(await screen.findByRole("button", { name: "index.html" }));
      const alert = await screen.findByRole("alert");
      expect(alert.textContent).toBe("打不开这个网页：browser.url_refused: outside the workspace");
      await waitFor(() => expect(document.querySelector(".cm-content")).toBeTruthy());
      expect(screen.getByRole("button", { name: "编辑" }).getAttribute("aria-pressed")).toBe("true");
    });

    it("explains a network path in the window's words", async () => {
      const user = userEvent.setup();
      const { port } = setup();
      refusedLikeTheKernel(port, "browser.network_path", "a network path");
      render(panel(port));
      await user.click(await screen.findByRole("button", { name: "index.html" }));
      const alert = await screen.findByRole("alert");
      expect(alert.textContent).toContain("网络上另一台机器");
      expect(alert.textContent).not.toContain("{");
    });
  });

  describe("what stays as it was", () => {
    it("opens other files in the editor and never asks the browser", async () => {
      const user = userEvent.setup();
      const { port, open } = setup();
      render(panel(port));
      await user.click(await screen.findByRole("button", { name: "notes.txt" }));
      await waitFor(() => expect(document.querySelector(".cm-content")).toBeTruthy());
      expect(screen.queryByRole("button", { name: "预览" })).toBeNull();
      expect(open).not.toHaveBeenCalled();
    });

    it("still renders Markdown as a document", async () => {
      const user = userEvent.setup();
      const { port, open } = setup();
      vi.spyOn(port, "workspaceFile").mockResolvedValue({ path: "README.md", content: "# Hello", revision: "r1" });
      render(panel(port));
      await user.click(await screen.findByRole("button", { name: "README.md" }));
      await waitFor(() => expect(document.querySelector(".workbench-read h1")?.textContent).toBe("Hello"));
      expect(screen.queryByRole("button", { name: "预览" })).toBeNull();
      expect(open).not.toHaveBeenCalled();
    });

    it("opens an HTML file in the editor where the window draws no pages", async () => {
      const user = userEvent.setup();
      const { port, open } = setup({ views: false });
      render(panel(port));
      await user.click(await screen.findByRole("button", { name: "index.html" }));
      await waitFor(() => expect(document.querySelector(".cm-content")).toBeTruthy());
      expect(screen.queryByRole("button", { name: "预览" })).toBeNull();
      expect(open).not.toHaveBeenCalled();
    });

    it("opens an HTML file in the editor when the workspace is on another machine", async () => {
      const user = userEvent.setup();
      const { port, open } = setup();
      render(panel(port, { remote: true }));
      await user.click(await screen.findByRole("button", { name: "index.html" }));
      await waitFor(() => expect(document.querySelector(".cm-content")).toBeTruthy());
      expect(open).not.toHaveBeenCalled();
    });

    it("keeps the agent's own page in the strip", async () => {
      const { port } = setup();
      render(panel(port, { tabs: [page({ target: "agent", url: "https://example.com", title: "Agent page", active: true })] }));
      expect(screen.getByRole("tab", { name: "Agent page" })).toBeTruthy();
    });

    it("leaves source and diff one click away", async () => {
      const user = userEvent.setup();
      const { port } = setup();
      render(panel(port));
      await user.click(await screen.findByRole("button", { name: "index.html" }));
      await user.click(await screen.findByRole("button", { name: "Diff" }));
      expect(screen.getByRole("button", { name: "Diff" }).getAttribute("aria-pressed")).toBe("true");
    });
  });
});
