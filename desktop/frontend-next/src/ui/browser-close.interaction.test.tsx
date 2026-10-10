// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import "./testkit";
import { MockPort } from "../port/mock";
import { WorkbenchPanel } from "./WorkbenchPanel";
import type { BrowserTab } from "../port/port";
import { host } from "../port/host";

afterEach(() => { cleanup(); vi.restoreAllMocks(); });
const tabs: BrowserTab[] = [
  { id: "b1", target: "v1", title: "First", url: "https://first.example", active: true },
  { id: "b2", target: "v2", title: "Second", url: "https://second.example", active: false },
];
function draw(close = vi.fn(async (_id: string) => {})) {
  const port = Object.assign(new MockPort(), { browserClose: close });
  const onCloseManual = vi.fn();
  render(<WorkbenchPanel port={port} tabs={tabs} manual={false} shown scheme="light" changes={[]} onCloseManual={onCloseManual} onSurfaces={vi.fn()} onExternal={vi.fn()} />);
  return { port, close, onCloseManual };
}
function menu(name: string) {
  fireEvent.contextMenu(screen.getByRole("button", { name }).closest('[role="tab"]')!, { clientX: 60, clientY: 60 });
}

describe("closing browser pages", () => {
  it("clears an open failure only when the selected tab closes", async () => {
    vi.spyOn(host(), "drawsBrowserViews").mockReturnValue(true);
    const { port } = draw();
    vi.spyOn(port, "browserOpen").mockRejectedValue(new Error("launch blocked"));
    fireEvent.click(screen.getByRole("button", { name: "First" }));
    fireEvent.click(screen.getByRole("button", { name: "新建浏览器标签" }));
    expect(await screen.findByRole("alert")).toHaveProperty("textContent", "launch blocked");
    fireEvent.click(screen.getByRole("button", { name: "关闭 Second" }));
    await waitFor(() => expect(screen.queryByRole("button", { name: "Second" })).toBeNull());
    expect(screen.getByRole("alert")).toHaveProperty("textContent", "launch blocked");
    fireEvent.click(screen.getByRole("button", { name: "关闭 First" }));
    await waitFor(() => expect(screen.queryByRole("alert")).toBeNull());
  });
  it("closes the actual page before removing its workbench tab", async () => {
    const { close } = draw();
    fireEvent.click(screen.getByRole("button", { name: "关闭 First" }));
    await waitFor(() => expect(close).toHaveBeenCalledWith("b1"));
    await waitFor(() => expect(screen.queryByRole("button", { name: "First" })).toBeNull());
    expect(screen.getByRole("button", { name: "Second" })).toBeTruthy();
  });
  it("closes other browser pages while preserving the menu's page", async () => {
    const { close } = draw();
    menu("First");
    fireEvent.click(screen.getByRole("menuitem", { name: "关闭其他浏览器标签" }));
    await waitFor(() => expect(close).toHaveBeenCalledWith("b2"));
    expect(close).not.toHaveBeenCalledWith("b1");
    expect(screen.getByRole("button", { name: "First" })).toBeTruthy();
  });
  it("closes all browser pages but keeps an open file", async () => {
    const { close, onCloseManual } = draw();
    fireEvent.click(await screen.findByRole("button", { name: "README.md" }));
    menu("First");
    fireEvent.click(screen.getByRole("menuitem", { name: "关闭全部浏览器标签" }));
    await waitFor(() => expect(close).toHaveBeenCalledTimes(2));
    await waitFor(() => expect(screen.queryByRole("button", { name: "First" })).toBeNull());
    expect(screen.getByRole("tab", { name: /README.md/ })).toBeTruthy();
    expect(onCloseManual).not.toHaveBeenCalled();
  });
  it("keeps a failed page visible and continues closing the remaining pages", async () => {
    const close = vi.fn(async (id: string) => { if (id === "b1") throw new Error("page is busy"); });
    draw(close);
    menu("First");
    fireEvent.click(screen.getByRole("menuitem", { name: "关闭全部浏览器标签" }));
    await waitFor(() => expect(close).toHaveBeenCalledWith("b2"));
    await waitFor(() => expect(screen.queryByRole("button", { name: "Second" })).toBeNull());
    expect(screen.getByRole("button", { name: "First" })).toBeTruthy();
    expect(screen.getByText(/page is busy/)).toBeTruthy();
  });
  it("dismisses the menu with Escape and restores focus to its tab", async () => {
    draw();
    const button = screen.getByRole("button", { name: "First" });
    button.focus();
    menu("First");
    fireEvent.keyDown(screen.getByRole("menu"), { key: "Escape" });
    await waitFor(() => expect(screen.queryByRole("menu")).toBeNull());
    expect(document.activeElement).toBe(button);
  });
});

it("preserves a newly selected page while another close is pending", async () => {
  let release: (() => void) | undefined;
  draw(vi.fn((_id: string) => new Promise<void>((resolve) => { release = resolve; })));
  fireEvent.click(screen.getByRole("button", { name: "关闭 First" }));
  fireEvent.click(screen.getByRole("button", { name: "Second" }));
  await act(async () => release?.());
  await waitFor(() => expect(screen.queryByRole("button", { name: "First" })).toBeNull());
  expect(screen.getByRole("tab", { name: /Second/ }).getAttribute("aria-selected")).toBe("true");
});
