// @vitest-environment jsdom
import { act, cleanup, render, renderHook, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import * as hostModule from "../port/host";
import { MockPort } from "../port/mock";
import type { BrowserTab } from "../port/port";
import { AgentBrowserPanel, UNREAD_TABS, useBrowserTabs } from "./BrowserPanel";
import { WorkbenchPanel } from "./WorkbenchPanel";

beforeEach(() => {
  vi.stubGlobal("ResizeObserver", class { observe() {} disconnect() {} unobserve() {} });
});
afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

const page = (target: string, url: string, title: string, active = true): BrowserTab => ({ id: target, target, url, title, active });
const drawsViews = (yes: boolean) => vi.spyOn(hostModule.host(), "drawsBrowserViews").mockReturnValue(yes);
const portWith = (read: () => Promise<BrowserTab[]>) => {
  const port = new MockPort();
  const spy = vi.spyOn(port, "browserTabs").mockImplementation(read);
  return { port, spy };
};

describe("agent pages on a shell that cannot draw them", () => {
  it("reads the agent's tabs and again whenever they move", async () => {
    drawsViews(false);
    let now = [page("a", "https://example.com/", "Example")];
    const { port, spy } = portWith(async () => now);
    const { result, rerender } = renderHook(({ moved }) => useBrowserTabs(port, moved), { initialProps: { moved: 0 } });
    expect(result.current).toBe(UNREAD_TABS);
    await waitFor(() => expect(result.current.map((p) => p.title)).toEqual(["Example"]));
    now = [page("a", "https://example.com/", "Example", false), page("b", "https://example.com/two", "Two")];
    rerender({ moved: 1 });
    await waitFor(() => expect(result.current).toHaveLength(2));
    now = [page("b", "https://example.com/two", "Two")];
    rerender({ moved: 2 });
    await waitFor(() => expect(result.current.map((p) => p.target)).toEqual(["b"]));
    expect(spy).toHaveBeenCalledTimes(3);
  });

  it("tells a read that found no pages from one not yet made, and keeps what it had when a read fails", async () => {
    drawsViews(false);
    let fail = false;
    const { port } = portWith(async () => {
      if (fail) throw new Error("offline");
      return [];
    });
    const { result, rerender } = renderHook(({ moved }) => useBrowserTabs(port, moved), { initialProps: { moved: 0 } });
    await waitFor(() => expect(result.current).not.toBe(UNREAD_TABS));
    expect(result.current).toEqual([]);
    const held = result.current;
    fail = true;
    rerender({ moved: 1 });
    await act(async () => {});
    expect(result.current).toBe(held);
  });

  it("reads the same on a shell that draws them", async () => {
    drawsViews(true);
    const { port } = portWith(async () => [page("a", "https://example.com/", "Example")]);
    const { result } = renderHook(() => useBrowserTabs(port, 0));
    await waitFor(() => expect(result.current).toHaveLength(1));
  });

  it("shows the page's title and address and offers nothing to do with it", () => {
    drawsViews(false);
    const show = vi.spyOn(hostModule.host(), "showBrowserView");
    render(<AgentBrowserPanel tabs={[page("a", "https://example.com/docs?q=1", "Example Domain")]} shown showTabs={false} />);
    expect(screen.getByText("Example Domain")).toBeTruthy();
    expect(screen.getByText("https://example.com/docs?q=1")).toBeTruthy();
    expect(screen.queryByRole("textbox")).toBeNull();
    expect(screen.queryByRole("button")).toBeNull();
    expect(screen.queryByRole("link")).toBeNull();
    expect(show).not.toHaveBeenCalled();
  });

  it("draws a tab with no title or address as a blank page", () => {
    drawsViews(false);
    render(<AgentBrowserPanel tabs={[page("a", "", "")]} shown showTabs={false} />);
    expect(screen.getByText("空白页")).toBeTruthy();
  });

  it("renders markup in a title or address as text, never as elements or links", () => {
    drawsViews(false);
    const title = `<img src=x onerror="alert(1)"><b>bold</b>`;
    const url = "javascript:alert(document.cookie)";
    const { container } = render(<AgentBrowserPanel tabs={[page("a", url, title)]} shown showTabs={false} />);
    expect(screen.getByText(title)).toBeTruthy();
    expect(screen.getByText(url)).toBeTruthy();
    expect(container.querySelector("img, a, [onerror], [data-page-title] *, [data-page-url] *")).toBeNull();
    expect(screen.queryByRole("link")).toBeNull();
  });

  it("shows data: addresses as plain text too", () => {
    drawsViews(false);
    const url = "data:text/html;base64,PHNjcmlwdD4=";
    const { container } = render(<AgentBrowserPanel tabs={[page("a", url, "x")]} shown showTabs={false} />);
    expect(screen.getByText(url)).toBeTruthy();
    expect(container.querySelector("a, iframe")).toBeNull();
  });

  it("cuts an address and a title that run on", () => {
    drawsViews(false);
    const url = "https://example.com/" + "a".repeat(5000);
    const title = "T".repeat(5000);
    const { container } = render(<AgentBrowserPanel tabs={[page("a", url, title)]} shown showTabs={false} />);
    const texts = [...container.querySelectorAll("[data-page-title], [data-page-url]")].map((el) => el.textContent ?? "");
    expect(texts).toHaveLength(2);
    for (const text of texts) {
      expect(text.length).toBeLessThan(600);
      expect(text.endsWith("…")).toBe(true);
    }
  });

  it("drops the characters that reorder what is shown", () => {
    drawsViews(false);
    render(<AgentBrowserPanel tabs={[page("a", "https://example.com/‮gpj.exe", "pay⁦pal⁩")]} shown showTabs={false} />);
    expect(screen.getByText("https://example.com/gpj.exe")).toBeTruthy();
    expect(screen.getByText("paypal")).toBeTruthy();
  });

  it("cuts on whole characters, never through a surrogate pair", () => {
    drawsViews(false);
    const title = "a".repeat(499) + "\u{1F600}" + "b".repeat(10);
    render(<AgentBrowserPanel tabs={[page("a", "https://example.com/", title)]} shown showTabs={false} />);
    const shown = document.querySelector("[data-page-title]")!.textContent!;
    expect(shown).toBe("a".repeat(499) + "\u{1F600}…");
    expect(/[\ud800-\udbff](?![\udc00-\udfff])|(?<![\ud800-\udbff])[\udc00-\udfff]/.test(shown)).toBe(false);
  });

  it.each([
    ["U+061C", "\u061C"], ["U+200B", "\u200B"], ["U+200D", "\u200D"], ["U+200F", "\u200F"], ["U+2060", "\u2060"],
    ["U+2064", "\u2064"], ["U+FEFF", "\uFEFF"], ["U+202E", "\u202E"], ["U+2066", "\u2066"], ["U+0000", "\u0000"],
    ["U+0007", "\u0007"], ["U+001B", "\u001B"], ["U+007F", "\u007F"], ["U+0085", "\u0085"], ["U+009F", "\u009F"],
  ])("drops %s from a title and an address", (_name, ch) => {
    drawsViews(false);
    render(<AgentBrowserPanel tabs={[page("a", `https://exa${ch}mple.com/`, `pay${ch}pal`)]} shown showTabs={false} />);
    expect(document.querySelector("[data-page-title]")!.textContent).toBe("paypal");
    expect(document.querySelector("[data-page-url]")!.textContent).toBe("https://example.com/");
  });

  it("folds runs of whitespace into one space", () => {
    drawsViews(false);
    render(<AgentBrowserPanel tabs={[page("a", "https://example.com/", "  one \n\t two\u00a0\u2003three  ")]} shown showTabs={false} />);
    expect(document.querySelector("[data-page-title]")!.textContent).toBe("one two three");
  });

  it("isolates direction: the address is always left to right and a title follows its own text", () => {
    drawsViews(false);
    render(<AgentBrowserPanel tabs={[page("a", "https://example.com/\u05D0", "\u05E9\u05DC\u05D5\u05DD page")]} shown showTabs={false} />);
    expect(document.querySelector("[data-page-url]")!.getAttribute("dir")).toBe("ltr");
    expect(document.querySelector("[data-page-title]")!.getAttribute("dir")).toBe("auto");
  });

  it("shows a look-alike host as the kernel read it", () => {
    drawsViews(false);
    render(<AgentBrowserPanel tabs={[page("a", "https://xn--pple-43d.com/", "Apple")]} shown showTabs={false} />);
    expect(screen.getByText("https://xn--pple-43d.com/")).toBeTruthy();
  });

  it("keeps the toolbar and the native view on a shell that draws pages", () => {
    drawsViews(true);
    render(<AgentBrowserPanel tabs={[page("a", "https://example.com/", "Example")]} shown showTabs={false} />);
    expect(screen.getByRole("textbox", { name: "网址" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "后退" })).toBeTruthy();
  });
});

describe("the workbench on a shell that cannot draw agent pages", () => {
  const mount = (tabs: BrowserTab[]) =>
    render(<WorkbenchPanel port={new MockPort()} tabs={tabs} manual={false} shown scheme="light" changes={[]} onCloseManual={vi.fn()} onSurfaces={vi.fn()} onExternal={vi.fn()} />);

  it("names the agent's page instead of showing the empty text", async () => {
    drawsViews(false);
    mount([page("a", "https://example.com/", "Example Domain")]);
    expect(await screen.findByText("https://example.com/")).toBeTruthy();
    expect(screen.getAllByText("Example Domain").length).toBeGreaterThan(0);
    expect(screen.queryByText("从右侧选择文件，或打开浏览器")).toBeNull();
  });

  it("keeps every page as a tab and shows the one picked", async () => {
    drawsViews(false);
    mount([page("a", "https://example.com/one", "One", false), page("b", "https://example.com/two", "Two")]);
    expect(await screen.findByText("https://example.com/two")).toBeTruthy();
    expect(screen.getByRole("tab", { name: /One/ })).toBeTruthy();
    expect(screen.getByRole("tab", { name: /Two/ })).toBeTruthy();
  });

  it("still shows the empty text when the agent has no page", () => {
    drawsViews(false);
    mount([]);
    expect(screen.getByText("从右侧选择文件，或打开浏览器")).toBeTruthy();
  });

  it("lets a page that closed go", () => {
    drawsViews(false);
    const view = mount([page("a", "https://example.com/", "Example Domain")]);
    view.rerender(<WorkbenchPanel port={new MockPort()} tabs={[]} manual={false} shown scheme="light" changes={[]} onCloseManual={vi.fn()} onSurfaces={vi.fn()} onExternal={vi.fn()} />);
    expect(screen.queryByText("https://example.com/")).toBeNull();
    expect(screen.getByText("从右侧选择文件，或打开浏览器")).toBeTruthy();
  });
});
